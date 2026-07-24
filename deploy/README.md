# HTTPS deployment

Caddy is the only service exposed to the host network. It terminates TLS and
proxies requests to the `app` Compose service on port 8080. The application
port must not be published on the host.

## Required environment

Set the production hostname in the server-side `.env` file:

```dotenv
DOMAIN=foodbox.o-r.kr
```

`DOMAIN` must be a hostname, not a URL. Its DNS `A` record must point to the
Oracle VM public IPv4 address. Add an `AAAA` record only when IPv6 is configured
and reachable on the VM.

Before starting Caddy, allow stateful inbound TCP traffic on ports 80 and 443
in both the Oracle Cloud NSG/security list and the host firewall. UDP 443 is
optional for HTTP/3. Outbound DNS and HTTPS access are required for certificate
issuance and renewal.

When the hostname resolves to the VM and ports 80 and 443 are reachable, Caddy
automatically obtains and renews the certificate and redirects HTTP to HTTPS.

## Persistent data

The Compose deployment must retain these mounts:

- `./db:/data` for the Foodbox file database
- `caddy_data:/data` for certificates, private keys, and other Caddy state
- `caddy_config:/config` for Caddy runtime configuration
- `./deploy:/etc/caddy:ro` for this configuration

Do not run `docker compose down -v` in production because it removes the Caddy
volumes. Back up `./db` before deployment. Only one `app` instance may run at a
time because the database is file-based and the app owns scheduled Slack jobs.

## Routing and security

- `/healthz` remains public and is also used by Caddy for active upstream health
  checks. It must be fast and must not call Slack, Clova, or the menu vendor.
- `/api/crawl`, `/api/upload`, and `/api/slack/notify` are intentionally returned
  as 404 at the public edge. Run equivalent administrative operations from the
  server or add application-level authentication before exposing them.
- Request bodies are limited to 10 MB, matching the existing upload limit.
- Vite assets under `/assets/` are cached for one year because their names are
  content-hashed. The root page and `index.html` are always revalidated.
- Access logs are emitted as JSON to stdout. Configure Docker log rotation in
  Compose so logs cannot fill the server disk.
- HSTS is enabled for this hostname only. Do not add `includeSubDomains` unless
  every subdomain is permanently available over HTTPS.

## Validate

Validate the Caddyfile without starting the production stack:

```bash
docker run --rm \
  -e DOMAIN=foodbox.o-r.kr \
  -v "$PWD/deploy:/etc/caddy:ro" \
  caddy:2-alpine \
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

After deployment, verify the public behavior:

```bash
curl --fail --show-error --location https://foodbox.o-r.kr/healthz
curl --fail --show-error https://foodbox.o-r.kr/api/menu
curl --head http://foodbox.o-r.kr/
curl --include https://foodbox.o-r.kr/api/crawl
```

The HTTP request should redirect to HTTPS, and the blocked administrative route
should return 404.
