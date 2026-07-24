# Foodbox

Foodbox downloads the Eisodosirak lunch menu image, parses it with Naver Clova
OCR, keeps the result in a file database, renders a Svelte calendar, and sends
daily Slack notifications.

Production runs as two small containers:

- one non-root Go application containing the API, scheduler, OCR pipeline, file
  store, and compiled Svelte assets;
- Caddy as the only host-facing service, providing automatic HTTPS and reverse
  proxying to the application.

## Requirements

- Go 1.26.5
- Node.js 24 and npm
- Docker with Docker Compose v2 for container testing or deployment

## Local development

Backend tests do not require production credentials:

```bash
cd backend
go test ./...
```

Build the frontend:

```bash
cd front
npm ci
npm run build
```

The Go process reads configuration only from its environment; it does not load
`.env` itself. Copy `.env.example` to the repository-local `.env`, replace every
placeholder outside version control, export that file in the shell, and then
run the application from `backend/`. When running from that directory, make
sure the database and static-directory variables resolve to the repository's
`db/` and `front/dist/` directories.

```bash
cd backend
set -a
source ../.env
set +a
DB_FILE_DIR=../db STATIC_DIR=../front/dist go run ./cmd/foodbox
```

For a production-like local build that bundles the frontend and uses the image
runtime defaults:

```bash
docker build --tag foodbox-local .
docker run --rm --env-file .env --publish 8080:8080 \
  --env DB_FILE_DIR=/data --env STATIC_DIR=/app/static \
  --volume foodbox-local-data:/data foodbox-local
```

Then check the readiness endpoint and menu API:

```bash
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/api/menu
```

## Verification

Run the full backend verification from `backend/`:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/foodbox
```

Run the frontend build from `front/`:

```bash
npm ci
npm run build
```

The Go tests deliberately pin behavior that users can observe. They cover the
file-database representation and upsert order, exact public API JSON, Slack
payload and Korean message text, 09:00 Asia/Seoul scheduling,
startup/schedule overlap, OCR date edge cases, and complete golden-menu output
for the retained OCR fixtures. Run both the ordinary and race suites before
changing persistence, crawling, OCR, API, scheduling, or Slack behavior.

Container and deployment changes should additionally render Compose, build the
Linux AMD64 image, and smoke-test `/healthz`, `/`, and `/api/menu`. CI performs
the clean-checkout backend, frontend, and container builds before a production
image is published.

## Configuration

Do not put credentials in source files, Docker images, commands, issue comments,
or GitHub repository variables. The table names the
variables only; secret values are intentionally omitted.

| Variable | Required | Purpose |
| --- | --- | --- |
| `CLOVA_URL` | Yes | Clova OCR invocation endpoint |
| `CLOVA_SECRET_KEY` | Yes | Clova OCR credential |
| `SLACK_TOKEN` | Yes | Incoming-webhook path secret, not a bot OAuth token |
| `SLACK_CHANNEL` | Yes | Slack destination channel |
| `CRAWL_URL` | No | Eisodosirak menu board URL |
| `SLACK_URL` | No | Slack webhook base URL |
| `SLACK_USERNAME` | No | Display name used by the Slack message |
| `ADMIN_TOKEN` | No | Internal management API credential; an empty value disables those routes |
| `SERVER_PORT` | No | Application listen port |
| `DB_FILE_DIR` | No | Directory containing `db.json` and `metadata.json` |
| `STATIC_DIR` | No | Compiled Svelte asset directory |
| `TZ` | No | Runtime timezone; production scheduling uses Seoul time |

Production keeps application configuration in the server-side `.env`. The
deployment script never replaces this file and restricts its permissions. It
creates a separate `.deploy.env` containing only deployment metadata needed by
Compose. `FOODBOX_IMAGE` and `DOMAIN` belong to that generated deployment file,
not to the application secret store.

## HTTP API

Public routes:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Verify that the file store is readable and writable |
| `GET` | `/api/menu` | Return all menus, newest first |
| `GET` | `/api/menu/today` | Return today's menu |

Management routes implemented by the application:

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/api/crawl` | Download, OCR, and persist the current menu |
| `POST` | `/api/upload` | Parse one multipart `file` upload, limited to 10 MB |
| `POST` | `/api/slack/notify` | Send today's notification immediately |

Management routes require `ADMIN_TOKEN` through a bearer authorization header
or `X-Admin-Token`. Production Caddy returns 404 for them before requests reach
the application, so possessing the token does not expose them on the public
internet. Add a separately authenticated operator path before changing that
edge policy.

Responses use the `{status,error,data}` envelope and the menu fields consumed by
the frontend. Error responses use the corresponding HTTP status. The
state-changing crawl and Slack routes require `POST`.

## Persistence and scheduling

- `db.json` stores dates as `[year,month,day]` alongside `menus` and `valid`.
  Disk records are oldest first; `/api/menu` is newest first.
- `metadata.json` persists the last successfully processed image hash, avoiding
  duplicate OCR work across restarts. A matching hash is skipped only while the
  database still contains today's menu, so missing state can repair itself.
- Writes use a unique temporary file, file and directory sync, and atomic
  replacement. Only one application instance may own the volume.
- Startup refresh runs after the HTTP server becomes ready and does not block
  readiness on vendor or Clova availability.
- Slack notification runs once daily at 09:00 Seoul time. Startup refresh and
  daily notification cannot overlap; a 09:00 tick during refresh is queued and
  sent after refresh completes.

## CI/CD

- `.github/workflows/ci.yml` tests Go, builds Svelte, and builds the Linux AMD64
  container for pull requests and development-branch changes.
- `.github/workflows/deploy.yml` repeats verification on `main`, publishes the
  application to GHCR, and deploys the exact image digest through the GitHub
  `production` Environment.
- `.github/workflows/rollback.yml` is a manually dispatched rollback to the
  previously successful Go release.

The Oracle VM never runs Go, Node, or Docker image builds during normal
deployment. It pulls an immutable digest, backs up the file database, starts the
stack, and accepts the release only after Compose health checks and the public
HTTPS health check succeed.

Application credentials are never sent by the workflow. They remain in the
Oracle VM's protected `.env`. The GitHub `production` Environment contains only
the SSH deployment secrets and optional `DEPLOY_PATH` / `PUBLIC_URL` variables.
The GHCR package must either be publicly readable or the VM must already be
logged in with a narrowly scoped read-only package credential.

A push to `main` starts deployment. If the `production` Environment has required
reviewers, the immutable image is built and published first, then the server
step waits for approval. The server locks concurrent releases, backs up
`db.json`, pulls the digest, waits for Compose and public HTTPS health checks,
and restores the starting release if validation fails.

After two successful deployments, the `Roll back production` workflow can be
manually dispatched from `main` to restore and verify the previously successful
digest. Never run `docker compose down -v` because the Caddy volumes contain
certificate state.
