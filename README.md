# CoreDNS Traefik Interface Plugin

A minimal CoreDNS plugin that creates DNS records for Traefik-discovered services using dynamic IP resolution from network interfaces.

## What it does

1. Polls Traefik API to discover hostnames from HTTP routes
2. Dynamically resolves IPs from a specified network interface
3. Creates A and AAAA records pointing to those IPs

## Configuration

```
traefik http://localhost:8080/api {
  interface tailscale0
  refreshinterval 30
  ttl 60
}
```

### Options

- `interface`: Network interface to get IPs from (required)
- `refreshinterval`: Traefik polling interval in seconds (default: 30)  
- `ttl`: DNS record TTL in seconds (default: 30)

## Example

For a Tailscale setup where Traefik exposes services:

```
nyx:53 {
  bind tailscale0
  
  traefik http://localhost:8080/api {
    interface tailscale0
    refreshinterval 30
    ttl 60
  }
  
  log
}
```

This will create DNS records for any hostnames Traefik knows about, pointing to the Tailscale IP address of the current machine.