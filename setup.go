package traefik

import (
	"net/url"
	"regexp"
	"strconv"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"

	"github.com/coredns/caddy"
)

const defaultTraefikApiEndpoint = "https://traefik.example.com/api"
const defaultTtl uint32 = 30
const defaultRefreshInterval uint32 = 30

func init() {
	caddy.RegisterPlugin("traefik", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func createPlugin(c *caddy.Controller) (*Traefik, error) {
	hostMatcher := regexp.MustCompile(`Host(SNI)?\(` + "`([^`]+)`" + `\)`)

	cfg := &TraefikConfig{
		interfaceName:   "",
		hostMatcher:     hostMatcher,
		ttl:             defaultTtl,
		refreshInterval: defaultRefreshInterval,
	}

	traefik := &Traefik{
		Config: cfg,
	}

	defaultBaseUrl, err := url.Parse(defaultTraefikApiEndpoint)
	if err != nil {
		return traefik, err
	}

	cfg.baseUrl = defaultBaseUrl

	mode := -1
	for c.Next() {
		args := c.RemainingArgs()
		if len(args) == 1 {
			baseUrl, err := url.Parse(args[0])
			if err != nil {
				return traefik, err
			}

			cfg.baseUrl = baseUrl
		}

		apiHostname := cfg.baseUrl.Hostname()
		cfg.apiHostname = apiHostname

		if len(args) > 1 {
			return traefik, c.ArgErr()
		}

		for c.NextBlock() {
			var value = c.Val()
			//goland:noinspection SpellCheckingInspection
			switch value {
			case "interface":
				if !c.NextArg() {
					return traefik, c.ArgErr()
				}
				cfg.interfaceName = c.Val()
				mode = 1 // Set mode to indicate A/AAAA records
			case "refreshinterval":
				if !c.NextArg() {
					return traefik, c.ArgErr()
				}
				refreshInterval, err := strconv.ParseUint(c.Val(), 10, 32)
				if err != nil {
					return traefik, err
				}
				if refreshInterval > 0 {
					cfg.refreshInterval = uint32(refreshInterval)
				}
			case "ttl":
				if !c.NextArg() {
					return traefik, c.ArgErr()
				}
				ttl, err := strconv.ParseUint(c.Val(), 10, 32)
				if err != nil {
					return traefik, err
				}
				if ttl > 0 {
					cfg.ttl = uint32(ttl)
				}
			case "fallthrough":
				traefik.Fall.SetZonesFromArgs(c.RemainingArgs())
			default:
				return traefik, c.Errf("unknown property: '%s'", c.Val())
			}
		}
	}

	if mode == -1 {
		return traefik, c.Errf("traefik config requires an interface")
	}

	traefikClient, err := NewTraefikClient(cfg)
	if err != nil {
		return nil, err
	}

	traefik.TraefikClient = traefikClient
	traefik.mappings = make(TraefikConfigEntryMap)

	log.Infof("base url ............ %s", cfg.baseUrl)
	log.Infof("interface ........... %v", cfg.interfaceName)
	log.Infof("ttl ................. %v", cfg.ttl)
	log.Infof("refreshInterval ..... %v", cfg.refreshInterval)
	log.Infof("apiHostname ......... %v", cfg.apiHostname)

	return traefik, nil
}

func setup(c *caddy.Controller) error {
	traefik, err := createPlugin(c)
	if err != nil {
		return err
	}

	go traefik.start()

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		traefik.Next = next
		return traefik
	})

	return nil
}
