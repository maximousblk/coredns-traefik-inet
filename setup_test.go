package traefik

import (
	"testing"

	"github.com/coredns/caddy"
)

// TestSetup tests the various things that should be parsed by setup.
// Make sure you also test for parse errors.
func TestSetup(t *testing.T) {
	c := caddy.NewTestController("dns", `traefik http://localhost:8080/api {
		interface eth0
	}`)
	if err := setup(c); err != nil {
		t.Fatalf("Expected no errors, but got: %v", err)
	}

	c = caddy.NewTestController("dns", `traefik http://localhost:8080/api`)
	if err := setup(c); err == nil {
		t.Fatalf("Expected errors (missing interface), but got: %v", err)
	}
}
