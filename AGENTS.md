# Foodbox agent guide

Keep this file short. Use `README.md` for development and API details and
`deploy/README.md` for production HTTPS operations.

## Root agent role

- The root agent orchestrates agents, communicates with the user, manages scope
  and decisions, and coordinates verification. It does not directly implement,
  investigate, modify files or servers, run tests, or create commits.
- Delegate implementation, research, file and server changes, test execution,
  and commits to subagents with clearly separated ownership. Run independent
  work in parallel when useful.
- During long-running work, brief the user about every two minutes with the
  active-agent count, approximate elapsed time, each assignment and current
  action, and any blockers.

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

## Behavior contracts

Treat externally visible API, persistence, OCR, and Slack behavior as stable.

- `db/db.json` keeps `{date:[year,month,day], menus:[...], valid:boolean}`.
- Database records are written oldest first; date upserts are last-write-wins;
  menu item order is preserved.
- Public menu JSON keeps the `{status,error,data}` envelope. API dates are ISO
  strings, validity is `isValid`, `/api/menu` is newest first, and empty lists
  are `[]`, never `null`.
- New menus are valid only with at least three items.
- Slack channel, username, `:bento:` icon, Korean weekday, bullets, whitespace,
  and full message bytes are contract-tested.
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

Management routes use POST, require `ADMIN_TOKEN`, and are blocked publicly by
Caddy. Error HTTP status matches the response envelope.

Do not weaken a contract test to make an implementation pass. Document and
test any explicitly approved behavior change.

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
changes must retain their contract tests. Report exact commands and
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

## Code and commits

- Prefer self-documenting code. If comments are necessary, use concise English;
  do not add Korean code comments.
- Never commit proactively. Commit only when the user explicitly asks.
- Use `type: imperative summary` with `feat`, `fix`, `chore`, `refactor`, `test`,
  `docs`, `build`, or `ci`; no trailing period.
- Before committing, inspect `git log --oneline -10`, status, and diff; stage
  only task files, run relevant verification, and confirm the resulting commit.
