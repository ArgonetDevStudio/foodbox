# Foodbox agent guide

Keep this file short. Use `README.md` for development and API details,
`docs/MIGRATION.md` for cutover and rollback, and `deploy/README.md` for HTTPS.

## How to work

- Surface assumptions, ambiguity, risks, and tradeoffs before making a choice.
- Prefer the smallest implementation that satisfies the current request. Do not
  add speculative abstractions, configuration, or compatibility layers.
- Make surgical changes: touch only lines and files required by the task.
- Define success with tests before or alongside behavior changes. Keep fixing
  and verifying until the relevant ordinary and race tests pass.
- Preserve unrelated working-tree changes. Never expose or overwrite secrets.

## Current architecture

- `backend/`: Go 1.26.5 API, scheduler, crawler, OCR, file store, Slack client,
  and static-file server
- `front/`: Svelte 5 UI, compiled into the Go container image
- `deploy/Caddyfile`: public HTTP/HTTPS edge and automatic certificates
- `docker-compose.yml`: one non-root Go app and one Caddy instance
- `.github/workflows/`: CI, immutable GHCR digest deployment, manual rollback
- `src/`, Gradle, and old Nginx files: temporary legacy reference only; the new
  image and workflows do not build them

The Oracle VM may still be running the Spring stack until cutover is accepted.
Check the server before describing the Go migration as live. A first-cutover
Spring rollback depends on preserved server Compose/JAR/images or Git history,
not the current root Dockerfile.

## Fragile compatibility contracts

Existing API and Slack users must not notice the runtime migration.

- `db/db.json` keeps `{date:[year,month,day], menus:[...], valid:boolean}`.
- Database records are written oldest first; date upserts are last-write-wins;
  menu item order is preserved.
- Public menu JSON keeps the `{status,error,data}` envelope. API dates are ISO
  strings, validity is `isValid`, `/api/menu` is newest first, and empty lists
  are `[]`, never `null`.
- New menus are valid only with at least three items.
- Slack channel, username, `:bento:` icon, Korean weekday, bullets, whitespace,
  and full message bytes must match the Java behavior.
- Notifications run at 09:00 Asia/Seoul. Preserve weekend/invalid skips,
  non-last-Wednesday `데니스델리 🥗`, and last-Wednesday `외식 🍽`.
- Startup refresh and notification never overlap. A tick during refresh is
  queued; a process starting at or after 09:00 schedules the next day rather
  than backfilling.
- OCR output must match all raw and golden fixtures in
  `backend/internal/ocr/testdata/`, including regions, confidence boundary,
  document order, year inference, OCR typo handling, and leap-day validity.
- A matching image hash may skip OCR only when today's menu still exists. Save
  the hash only after OCR and database persistence succeed.

Intentional HTTP changes are limited to management routes: crawl and Slack
notify are POST, all management routes require `ADMIN_TOKEN`, and Caddy blocks
them publicly. Go error HTTP status matches the envelope even where legacy
Spring returned HTTP 200.

Do not weaken a parity test to make an implementation pass. Document and test
both retained behavior and any explicitly approved exception.

## Required verification

From `backend/`:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/foodbox
```

From `front/`:

```bash
npm ci
npm run build
```

For container or deployment changes, also build the Linux AMD64 image, render
Compose, validate Caddy, and smoke-test `/healthz`, `/`, and `/api/menu`. OCR
changes must run every golden fixture. Persistence, API, Slack, and scheduler
changes must retain their legacy contract tests. Report exact commands and
results; do not claim checks that were not run.

## Secrets and production safety

- Runtime configuration comes only from environment variables; Go does not load
  `.env`. Production application secrets stay in the Oracle VM `.env`.
- `SLACK_TOKEN` is the incoming-webhook path after
  `https://hooks.slack.com/services/`, not an `xoxb` OAuth token.
- `CLOVA_URL`, `CLOVA_SECRET_KEY`, `SLACK_TOKEN`, and `SLACK_CHANNEL` are required.
  `ADMIN_TOKEN` is optional; empty disables management APIs.
- `FOODBOX_IMAGE` and `DOMAIN` belong to deploy-generated `.deploy.env`, not the
  application `.env`. GitHub's `production` Environment contains SSH deployment
  secrets and optional `DEPLOY_PATH` / `PUBLIC_URL`, not app credentials.
- Never log configuration structs, credential-bearing URLs, or secret values.
  Clova and Slack require HTTPS, must not follow redirects, and need bounded
  requests, responses, and timeouts.
- Only one app replica may own the file database. Preserve `db/`, backups,
  `.env`, `.deploy.env`, and Caddy volumes. Never run `docker compose down -v`
  in production.
- Normal deployment builds nothing on the VM. It pulls an immutable digest,
  backs up the DB, waits for local and public HTTPS health, and restores the
  starting release on failure.

## Legacy removal gate

Go tests are self-contained: the six Java-era OCR fixtures have byte-identical
copies under `backend/internal/ocr/testdata/` and no backend test reads
`src/test/resources/`. Still, delete Java/Gradle/old Nginx files only after:

1. Go ordinary/race/vet/build, parity, frontend, and container checks pass.
2. Production preserves DB, JSON, UI, HTTPS, and a real 09:00 Slack notification
   through the agreed observation window.
3. Go rollback is verified, credentials are rotated, and the user explicitly
   closes the first-cutover rollback window.

Perform legacy deletion as a separate cleanup, rerun every check, and update
the README and migration runbook. Never delete live state or protected backups.

## Code and commits

- Prefer self-documenting code. If comments are necessary, use concise English;
  do not add Korean code comments.
- Never commit proactively. Commit only when the user explicitly asks.
- Use `type: imperative summary` with `feat`, `fix`, `chore`, `refactor`, `test`,
  `docs`, `build`, or `ci`; no trailing period.
- Before committing, inspect `git log --oneline -10`, status, and diff; stage
  only task files, run relevant verification, and confirm the resulting commit.
