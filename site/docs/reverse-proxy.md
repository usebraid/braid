---
title: Reverse proxy
order: 4
group: Run the server
description: Put TLS in front of Braid with Caddy, Traefik or nginx.
---

# Reverse proxy

Braid listens on `BRAID_LISTEN` (default `:8080`) and speaks plain HTTP. Put a reverse proxy with TLS in front of it. `BRAID_PUBLIC_URL` must be the `https://` URL users and devices use; Braid refuses plain `http` for anything but localhost.

The proxy has to:

- pass the WebSocket upgrade for `/api/v1/live` (the live view);
- keep the `Host` and `Origin` headers as the client sent them (Braid checks the `Origin` of the live connection and of state-changing requests against `BRAID_PUBLIC_URL`);
- set `X-Forwarded-For`.

## Trusted proxies

`BRAID_TRUSTED_PROXIES` is a comma-separated list of CIDRs, for example `172.18.0.0/16,10.0.0.5/32`. It is empty by default, so `X-Forwarded-For` is ignored and every request appears to come from the proxy. When the direct peer is inside one of the CIDRs, Braid takes the rightmost `X-Forwarded-For` hop that is not itself trusted as the client address (the leftmost if all are). It is used for rate limits and the audit log. List only your own proxies: anything listed can forge client addresses.

## Caddy

```
braid.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Caddy gets the certificate itself, upgrades WebSockets and sets `X-Forwarded-For` by default.

## Traefik

```yaml
http:
  routers:
    braid:
      rule: Host(`braid.example.com`)
      entryPoints: [websecure]
      tls: {certResolver: letsencrypt}
      service: braid
  services:
    braid:
      loadBalancer:
        servers:
          - url: http://127.0.0.1:8080
```

Traefik forwards WebSockets and sets `X-Forwarded-For` without extra configuration.

## nginx

```
map $http_upgrade $connection_upgrade { default upgrade; '' close; }

server {
    listen 443 ssl;
    server_name braid.example.com;
    # ssl_certificate / ssl_certificate_key as usual

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 1h;
    }
}
```

## TLS

Use a certificate from your CA of choice; Let's Encrypt via the proxy is the usual way. Devices talk to `BRAID_PUBLIC_URL` over HTTPS and verify the certificate.

## Beyond the proxy

Intrusion banning (fail2ban, CrowdSec), disk encryption and firewalling are your responsibility; Braid has no end-to-end encryption in v1 (see `SECURITY.md` in the repository). Use the audit log as a source for banning rules.
