package traefik

import (
	"context"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
)

func TestExample(t *testing.T) {
	// Create a new Example Plugin with minimal configuration. Use the test.ErrorHandler as the next plugin.
	x := Traefik{
		Next: test.ErrorHandler(),
		Config: &TraefikConfig{
			interfaceName: "eth0",
			ttl:           30,
		},
		mappings: make(TraefikConfigEntryMap),
	}

	ctx := context.TODO()
	r := new(dns.Msg)
	r.SetQuestion("example.org.", dns.TypeA)
	// Create a new Recorder that captures the result, this isn't actually used in this test
	// as it just serves as something that implements the dns.ResponseWriter interface.
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	// Call our plugin directly, and check the result.
	code, err := x.ServeDNS(ctx, rec, r)
	
	// Since we don't have any mappings configured, it should return success but with NXDOMAIN
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("Expected RcodeSuccess, got %d", code)
	}
	if rec.Msg.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN in response, got %d", rec.Msg.Rcode)
	}
}
