package traefik

import (
	"context"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/coredns/coredns/plugin/pkg/fall"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

var log = clog.NewWithPlugin("traefik")

// getIPsFromInterface retrieves IPv4 and IPv6 addresses from the specified network interface
func getIPsFromInterface(ifaceName string) ([]net.IP, []net.IP) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		log.Errorf("Failed to find interface %s: %v", ifaceName, err)
		return nil, nil
	}

	addrs, err := iface.Addrs()
	if err != nil {
		log.Errorf("Failed to get addresses for interface %s: %v", ifaceName, err)
		return nil, nil
	}

	var v4s, v6s []net.IP
	for _, addr := range addrs {
		var ip net.IP
		if ipNet, ok := addr.(*net.IPNet); ok {
			ip = ipNet.IP
		}

		if ip.To4() != nil && !ip.IsLoopback() {
			v4s = append(v4s, ip)
		} else if ip.To16() != nil && !ip.IsLoopback() && ip.To4() == nil {
			v6s = append(v6s, ip)
		}
	}
	return v4s, v6s
}

//goland:noinspection GoNameStartsWithPackageName
type TraefikConfigEntry struct {
	ttl uint32
}

//goland:noinspection GoNameStartsWithPackageName
type TraefikConfigEntryMap map[string]*TraefikConfigEntry

//goland:noinspection GoNameStartsWithPackageName
type TraefikConfig struct {
	baseUrl         *url.URL
	interfaceName   string
	ttl             uint32
	refreshInterval uint32
	hostMatcher     *regexp.Regexp
	apiHostname     string
}

type Traefik struct {
	Next          plugin.Handler
	Config        *TraefikConfig
	TraefikClient *TraefikClient

	mappings TraefikConfigEntryMap
	ready    bool
	mutex    sync.RWMutex
	Fall     fall.F
}

func (t *Traefik) Name() string { return "traefik" }

func (t *Traefik) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	if state.QClass() != dns.ClassINET || (state.QType() != dns.TypeA && state.QType() != dns.TypeAAAA) {
		return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
	}

	requestCount.WithLabelValues(metrics.WithServer(ctx)).Inc()

	qname := state.QName()
	answers := []dns.RR{}

	// Check if the plugin has a mapping for the requested host
	find := strings.ToLower(qname)
	if strings.HasSuffix(find, ".") {
		find = find[:len(find)-1]
	}

	result := t.getEntry(find)
	if result != nil {
		// IP lookup is now dynamic for every request
		ipv4s, ipv6s := getIPsFromInterface(t.Config.interfaceName)

		switch state.QType() {
		case dns.TypeA:
			if len(ipv4s) > 0 {
				answers = a(qname, t.Config.ttl, ipv4s)
			}
		case dns.TypeAAAA:
			if len(ipv6s) > 0 {
				answers = aaaa(qname, t.Config.ttl, ipv6s)
			}
		}
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	if len(answers) == 0 {
		if t.Fall.Through(qname) && t.Next != nil {
			log.Debug("Falling through. 0 answers")
			return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
		}

		log.Debug("Returning NXDOMAIN")
		m.Rcode = dns.RcodeNameError
	}

	//goland:noinspection ALL
	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

func (t *Traefik) start() error {
	log.Info("Starting!")
	err := t.refresh(true)

	if err != nil {
		log.Warningf("Failed to load Traefik HTTP routers, will retry: %s", err)
	}

	uptimeTicker := time.NewTicker(time.Duration(t.Config.refreshInterval) * time.Second)

	for {
		select {
		case <-uptimeTicker.C:
			log.Debug("Refreshing sites")
			err := t.refresh(false)
			if err != nil {
				log.Warningf("Error loading Traefik HTTP routers: %v", err)
			}
		}
	}
}

func (t *Traefik) getEntry(host string) *TraefikConfigEntry {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	value, foundIt := t.mappings[host]
	if !foundIt {
		return nil
	}

	return value
}

func (t *Traefik) refresh(first bool) error {
	if first {
		log.Infof("Checking for Traefik HTTP routers...")
	}
	routers, err := t.TraefikClient.GetHttpRouters()
	if err != nil {
		log.Errorf("Error retrieving Traefik HTTP routers: %s", err)
		return err
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	adds, deletes := 0, 0
	fromTraefik := map[string]struct{}{}
	for _, s := range *routers {
		//goland:noinspection ALL
		strs := t.Config.hostMatcher.FindAllStringSubmatch(s.Rule, -1)
		for _, s := range strs {
			if len(s) == 3 {
				host := strings.ToLower(s[2])
				fromTraefik[host] = struct{}{}

				_, exists := t.mappings[host]
				if !exists {
					log.Infof("+ %s -> %s", host, t.Config.interfaceName)
					t.mappings[host] = &TraefikConfigEntry{
						ttl: t.Config.ttl,
					}
					adds += 1
				}
			}
		}
	}

	toDelete := map[string]struct{}{}
	for cachedHost := range t.mappings {
		_, stillExists := fromTraefik[cachedHost]
		if !stillExists {
			log.Infof("- %s -> %s", cachedHost, t.Config.interfaceName)
			toDelete[cachedHost] = struct{}{}
			deletes += 1
		}
	}

	for del := range toDelete {
		delete(t.mappings, del)
	}

	if adds > 0 && deletes > 0 {
		log.Infof("Added %d, deleted %d entries", adds, deletes)
	} else if adds > 0 {
		log.Infof("Added %d entries", adds)
	} else if deletes > 0 {
		log.Infof("Deleted %d entries", deletes)
	} else {
		if first {
			log.Warning("Failed to load Traefik HTTP routes... Will try again")
		} else {
			log.Debug("No changes detected")
		}
	}

	t.ready = true
	return nil
}

func a(zone string, ttl uint32, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}
		r.A = ip
		answers[i] = r
	}
	return answers
}

func aaaa(zone string, ttl uint32, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.AAAA)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl}
		r.AAAA = ip
		answers[i] = r
	}
	return answers
}


