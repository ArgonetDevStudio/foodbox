# HTTPS deployment

Caddy is the only service exposed to the host network. It terminates TLS and
proxies requests to the `app` Compose service on port 8080. The application
port must not be published on the host.

## Required environment

Set the HTTPS origin as the GitHub production environment variable
`PUBLIC_URL`:

```text
PUBLIC_URL=https://foodbox.o-r.kr
```

This Compose layout publishes Caddy on the standard HTTPS port, so omit an
explicit port from `PUBLIC_URL`.

The deploy workflow passes `PUBLIC_URL` to `scripts/deploy.sh`. The script
validates that it is an HTTPS origin without a path, extracts its hostname, and
atomically writes the immutable image digest and `DOMAIN` to the server-side
`.deploy.env` file. Compose reads `.deploy.env` with `--env-file` and passes
`DOMAIN` to Caddy. Do not add `DOMAIN` to the application secrets file `.env` or
edit `.deploy.env` manually.

The hostname extracted from `PUBLIC_URL` must have a DNS `A` record pointing to
the Oracle VM public IPv4 address. Add an `AAAA` record only when IPv6 is
configured and reachable on the VM.

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

The deployment also retains the generated `.deploy.env` file for Compose and
rollback state. Application secrets remain separately in `.env`; neither file
belongs in source control.

Do not run `docker compose down -v` in production because it removes the Caddy
volumes. Back up `./db` before deployment. Only one `app` instance may run at a
time because the database is file-based and the app owns scheduled Slack jobs.

## Automated release and rollback

Merging to `main` runs the release verification and image build, then enters
the GitHub `production` environment before changing the VM. Configure a
required reviewer on that environment to make deployment wait for explicit
approval. The VM job is detached from SSH and can be reattached by re-running
the same workflow run. Do not start a second manual operation while a job
reports `RUNNING` or `EXIT:21`.

Every deployment stops and verifies the current stack before taking its final
database snapshot or starting the Go writer. On an initial installation without
a previous Go release, a failed Go release is stopped, the complete final
snapshot and original configuration-existence state are restored, and no
runtime is started (`exit 12`). For upgrades between Go releases, failure
restores the exact previous digest and reports `exit 10` only after health, API,
UI, database, and single-writer checks pass. Valid rows added after the snapshot
are retained; the snapshot is restored only when database integrity fails. An
unprovable stop or failed recovery reports `exit 11` and requires inspection
before another operation.

The manual rollback workflow is Go-only and swaps the active release with the
stored previous immutable digest. It refuses to run when a crashed deployment
or rollback left unresolved transaction state under `.deploy-state`. Resolve
that state on the VM before retrying; do not delete it merely to bypass the
safety check. Exit `20` is a preflight rejection, and `21` means a detached job
lost its definitive result. Do not delete transaction state or start another
runtime after exit `11` or `21` until the running containers and database have
been inspected.

## Routing and security

- `/healthz` remains public and is also used by Caddy for active upstream health
  checks. It must be fast and must not call Slack, Clova, or the menu vendor.
- `/api/crawl`, `/api/upload`, and `/api/slack/notify` are intentionally returned
  as 404 at the public edge. Run equivalent administrative operations from the
  server or add application-level authentication before exposing them.
- Request bodies are limited to the configured 10 MB upload limit.
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
  caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648 \
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

For manual Compose diagnostics on the server, use the generated deployment
environment explicitly:

```bash
docker compose --env-file .deploy.env config --quiet
docker compose --env-file .deploy.env ps
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
