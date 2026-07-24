# Spring-to-Go production migration

This runbook covers the one-time Oracle VM migration from the legacy Spring
Boot and Nginx stack to the Go application and Caddy stack, followed by normal
digest-based deployments and rollbacks.

No database service is introduced. The existing `db/db.json` remains the source
of truth.

## Security actions before deployment

Complete these actions before publishing or deploying another artifact:

1. Rotate the active Clova OCR credential.
2. Rotate the active Slack incoming webhook.
3. Retire the different historical Slack webhook embedded in the legacy server
   JAR if it is still valid.
4. Store replacements only in the server-side `.env` and an approved local
   secret manager. Do not paste them into GitHub variables, workflow inputs,
   chat, issues, or documentation.
5. Remove literal credentials from the ignored local
   `src/main/resources/application-dev.yml`. Remove stale local
   `build/resources`, fat JARs, and other generated artifacts after rotation.
6. Treat the legacy server JAR, Docker image layers, Docker JSON logs, backups,
   and VM snapshots as secret-bearing until old credentials are revoked and the
   retention window ends.

The public Git repository has no tracked history of `application-dev.yml` or
`.env`; a history rewrite is not required for this incident. The exposure is in
ignored working-tree files, generated JARs/images, and legacy container logs.

## What changes at cutover

| Legacy production | New production |
| --- | --- |
| Spring Boot JVM backend | Statically compiled Go backend |
| Separate Svelte/Nginx container | Svelte assets bundled in the Go image |
| Manually managed Nginx certificates | Caddy automatic certificate management |
| Images built on the Oracle VM | Image built by GitHub Actions and pulled by digest |
| Host `./db` mounted at `/db` | Host `./db` mounted at `/data` |
| In-memory duplicate-image hash | Persistent `metadata.json` beside `db.json` |
| State-changing management GET routes | Authenticated POST routes blocked at the public edge |
| Error code mainly in JSON body | Matching JSON and HTTP error status |

The menu database needs no conversion. Go reads and writes the legacy
`[year,month,day]`, `menus`, and `valid` representation. It creates
`metadata.json` independently; legacy Spring ignores that file. A rollback may
normally continue with the current `db.json` rather than discarding menus added
after cutover.

## One-time prerequisites

### DNS and network

- Point the production hostname's IPv4 record at the Oracle VM.
- Publish an IPv6 record only when the VM is actually reachable over IPv6.
- Permit inbound TCP 80 and 443 in the Oracle Cloud NSG or security list and in
  the host firewall.
- Permit outbound DNS and HTTPS for Caddy, GHCR, the menu vendor, Clova, and
  Slack.
- UDP 443 is optional and enables HTTP/3.

Caddy is the only service with published host ports. The application port must
remain internal to the Compose network.

### Oracle VM

The deployment account needs:

- key-based SSH access;
- permission to use Docker and Docker Compose v2;
- `bash`, `flock`, and `curl`;
- write access to the deployment directory;
- enough disk for current and previous images, database backups, and Caddy
  state.

Keep the existing server-side `.env`. Confirm that it contains every required
application variable listed in the root README, that its mode is restricted,
and that it contains the rotated credentials. The workflow does not copy
application secrets to the server.

The deploy script creates and owns:

- `.incoming/` for staged release files;
- `.deploy.env` for the immutable image reference and Caddy hostname;
- `.deploy-state/` for locking and the previous successful release;
- `backups/` for timestamped database copies;
- `deploy/` and `scripts/` for active release files.

Do not edit `.deploy.env` manually.

### GHCR access

GitHub Actions publishes the Foodbox container under the repository owner's
GHCR namespace and deploys its exact digest. Choose one access model before the
first run:

- make the package public so the Oracle VM can pull anonymously; or
- keep it private and log the VM's Docker client into GHCR with a narrowly
  scoped read-only package credential.

The workflow's repository token publishes the image but is not installed on
the VM. A private package therefore requires a separate server-side registry
login. Never store that registry credential in a repository variable.

## GitHub `production` Environment

Create a GitHub Environment named `production`. Configure required reviewers if
production deployment must wait for manual approval.

Environment secrets:

| Name | Required | Purpose |
| --- | --- | --- |
| `DEPLOY_HOST` | Yes | Oracle VM SSH host |
| `DEPLOY_USER` | Yes | Restricted deployment account |
| `DEPLOY_SSH_KEY` | Yes | Private key used only by GitHub Actions |
| `DEPLOY_KNOWN_HOSTS` | Yes | Pre-verified SSH host-key record |
| `DEPLOY_PORT` | No | Non-default SSH port when applicable |

Environment variables:

| Name | Required | Purpose |
| --- | --- | --- |
| `DEPLOY_PATH` | No | Absolute Foodbox directory on the VM |
| `PUBLIC_URL` | No | HTTPS origin used for links and health checks |

Verify the SSH host-key fingerprint through an independent trusted channel
before storing `DEPLOY_KNOWN_HOSTS`. Do not accept an unverified first response
from the same network path the workflow will use.

Clova, Slack, and `ADMIN_TOKEN` are application secrets and remain in the VM's
`.env`; they do not belong in the GitHub deployment Environment.

## Pre-cutover backup and rollback kit

Before merging the production change:

1. Stop manual crawl, upload, and Slack-notify operations for the maintenance
   window. Only one process may write the file database.
2. Copy `db/db.json` to a timestamped, access-restricted backup and verify that
   the backup parses as JSON.
3. Back up the server `.env` to an encrypted or otherwise restricted location.
   Never print it during verification.
4. Save the legacy Compose file and record the legacy backend and frontend
   image IDs.
5. Keep the legacy fat JAR and images only for the short first-cutover rollback
   window. They remain secret-bearing artifacts after credential rotation.
6. Confirm available disk space, Docker health, and that no second Foodbox stack
   is running.
7. Confirm the existing `db/` directory is the mount used by the legacy
   container.

The deployment script creates another `db.json` backup before changing the
release. It may migrate ownership of the bind-mounted directory to the non-root
application UID/GID and verifies writability before starting Compose.

Do not use `docker compose down -v`. The named Caddy volumes contain certificate
and runtime state.

## Cutover

1. Merge the verified migration change to `main`.
2. The deployment workflow runs Go tests, builds Svelte, publishes a Linux AMD64
   image with provenance and SBOM, and captures its immutable digest.
3. If the `production` Environment has reviewers, approve deployment only after
   confirming the backup and credential-rotation checklist.
4. The workflow stages only Compose, Caddy, and deployment scripts over strict
   host-key-checked SSH.
5. The server pulls the digest before modifying the active release, validates
   the database mount, installs release files, and starts Compose.
6. Compose waits for application readiness before starting Caddy.
7. The release is accepted only after the public HTTPS `/healthz` request
   succeeds. A failure restores the release files active before the attempt.

The stack intentionally runs one application replica with memory and PID
limits, non-root application ownership, `no-new-privileges`, and bounded Docker
JSON-log rotation.

## Post-cutover verification

Verify without exposing credentials:

```bash
docker compose --env-file .deploy.env config --quiet
docker compose --env-file .deploy.env ps
docker compose --env-file .deploy.env logs --tail 100 app caddy
curl --fail --show-error --location "$PUBLIC_URL/healthz"
curl --fail --show-error "$PUBLIC_URL/api/menu"
curl --include "$PUBLIC_URL/api/crawl"
```

Expected results:

- the application and Caddy containers are healthy;
- HTTP redirects to HTTPS and the certificate is valid;
- `/healthz` reports readiness without calling external services;
- `/api/menu` preserves the response envelope and existing menu data;
- the public management route returns 404;
- `db/db.json` still contains existing history;
- `db/metadata.json` appears after a successful crawl;
- no credential values appear in application or Caddy logs;
- the Oracle VM did not run Go, Node, Gradle, or Docker builds.

Also confirm the next scheduled Slack notification or perform a controlled
internal test through an authenticated operator path. Management endpoints are
not reachable through public Caddy by design.

## First-cutover rollback to Spring

The automated rollback workflow requires a previous successful digest-based Go
release. On the first migration from the Spring Compose file,
`.deploy-state/previous.env` does not yet exist, so use this manual rollback:

1. Preserve a fresh copy of the current `db.json` before changing containers.
2. Stop the new Compose stack without deleting named volumes.
3. Restore the saved legacy Compose file.
4. Confirm the legacy JAR and local Docker images are still available.
5. Start exactly one legacy stack and verify its menu API before re-enabling
   scheduled notifications.
6. Keep the current database unless investigation shows corruption. Its schema
   remains Spring-compatible. Restore the pre-cutover backup only when
   necessary, because doing so discards menus written after the backup.
7. Continue using rotated runtime credentials from the protected `.env`; never
   restore credentials from the legacy JAR or logs.

If ownership migration prevents a non-container operator from reading backups,
correct only the required backup permissions. The legacy container historically
ran as root and can use the Go-written database format.

Record the rollback reason and preserve relevant sanitized logs before retrying
the migration.

## Rollback between Go releases

After at least two successful Go deployments, run the `Roll back production`
workflow manually from `main`. The server-side rollback script:

1. validates the stored previous immutable digest;
2. locks out concurrent deployments;
3. backs up `db.json` again;
4. restores the previous Compose, Caddy, and deployment environment files;
5. pulls and starts the previous digest;
6. requires Compose readiness and the public HTTPS health check;
7. restores the newer release if rollback itself fails.

Successful rollback swaps stored release state, so another rollback toggles
back to the release active before it. It is a one-release rollback mechanism,
not an indefinite release archive.

## End of migration window

After the Go release has operated successfully through the agreed observation
window:

- confirm the rotated Clova and Slack credentials are the only active ones;
- remove legacy containers, JARs, local images, stale build directories, and
  secret-bearing Docker logs;
- remove protected rollback copies after their retention period;
- retain database backups according to the operational retention policy;
- keep Caddy's named volumes;
- decide separately whether to delete the legacy Java source and tests.

Removing Java source is not required for runtime memory savings: the current
Dockerfile and workflows do not compile or ship it.
