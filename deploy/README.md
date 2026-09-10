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

Every push to `main` starts the deploy workflow. Its release verification must
pass before it builds and publishes an immutable image. The server job then
enters the GitHub `production` Environment; configure a required reviewer there
to require explicit deployment approval. PR approval is separate and is
enforced only by the repository's branch ruleset.

The VM job runs detached from SSH. Re-running the same workflow run reattaches
to the durable job instead of starting the operation again. Do not start a
second operation while a job reports `RUNNING` or after `EXIT:21`.

Every deployment snapshots the database before stopping the current stack,
proves that all starting containers stopped, and takes a final stopped-state
snapshot before starting the new writer. If no previous successful Go release
exists, failure stops the target, restores that exact final database snapshot
and the original configuration-existence state, and remains stopped. If a
previous Go release exists, failure restores its exact immutable configuration
and verifies runtime, API, UI, database, and single-writer health. Rows safely
added after the snapshot are retained; the complete snapshot is restored only
when database integrity cannot be preserved.

The manual rollback workflow swaps the active release with the stored previous
immutable Go release. It refuses to run while unresolved deployment or rollback
transaction state exists under `.deploy-state`; inspect and resolve that state
rather than deleting it to bypass the guard.

The detached operation reports these final results:

| Exit | Meaning |
| --- | --- |
| `0` | Deployment or rollback completed and passed its verification checks. |
| `2` | The invocation, durable-job request, or staged bundle is invalid. |
| `10` | The operation did not complete, but the Go release active at its start was restored and verified. |
| `11` | Automatic safety or recovery could not be completed or proved; inspect the VM before another operation. |
| `12` | No previous successful Go release existed; DB/config were restored exactly and the stack remains stopped. |
| `20` | Preflight or safety checks rejected the operation before changing the active release. |
| `21` | The detached job lost its definitive result; inspect the VM and durable job state before continuing. |

## Routing and security

- `/healthz` remains public and is also used by Caddy for active upstream health
  checks. It must be fast and must not call Slack, Clova, or the menu vendor.
- `/api/crawl`, `/api/upload`, `/api/menu/manual`, and `/api/slack/notify` are intentionally returned
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
