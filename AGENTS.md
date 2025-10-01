# CLAUDE.md

This file provides guidance to AI Agents when working with code in this repository.

## Project Overview

Foodbox is a lunch menu notification system that crawls a food vendor's website daily, structures the menu data, and sends notifications via Slack. It consists of a Spring Boot backend (Java 21) and a Svelte frontend served via Nginx, deployed together using Docker Compose.

## Architecture

### Backend (Spring Boot)
- **Main Package**: `src/main/java/shanepark/foodbox/`
- **Key Modules**:
  - `api/`: REST API layer with controllers, services, repositories, domain models, and configuration
  - `crawl/`: HTML parsing logic using JSoup to extract menu data from vendor website
  - `slack/`: Slack integration for automated notifications

### Frontend (Svelte)
- **Location**: `front/` directory
- **Tech Stack**: Svelte 5 + Vite
- **Deployment**: Static build served by Nginx, proxies `/api/*` requests to backend

### Data Flow
1. Backend crawls vendor website HTML using JSoup (not OCR-based)
2. Menu data is parsed and stored in file-based database (`./db/` directory)
3. REST API exposes menu data to frontend
4. Slack bot posts daily notifications on a schedule
5. Frontend (Nginx on port 80) proxies API requests to backend service

## Common Commands

### Local Development

**Backend:**
```bash
./gradlew clean build           # Build the project
./gradlew bootRun              # Run backend locally
./gradlew test                 # Run all tests
./gradlew test --tests MenuCrawlerTest  # Run specific test
```

**Frontend:**
```bash
cd front
npm install                    # Install dependencies
npm run dev                    # Start dev server with hot reload
npm run build                  # Build for production
```

### Docker Deployment

```bash
# Build and start both services
./gradlew clean build          # Must build backend first
docker compose up -d           # Start services (frontend on port 80)

# View logs
docker compose logs -f foodbox-backend
docker compose logs -f foodbox-frontend

# Rebuild after changes
docker compose build
docker compose up -d
```

### Testing

The project uses file-based HTML samples in `src/test/resources/` for realistic crawler testing. Tests use JUnit 5, Mockito, and AssertJ.

## Configuration

### Environment Variables
Required environment variables (see `.env.example`):
- `SLACK_TOKEN`: Slack bot token for notifications
- `SLACK_CHANNEL`: Target Slack channel (e.g., #lunch)
- `CRAWL_URL`: Vendor website URL to crawl (default: http://www.msmfood.co.kr/page/sub2_7)
- `DB_FILE_DIR`: Database file storage path (default: `/foodbox/db` in Docker, `./db` locally)

### Application Configuration
Configuration is in `src/main/resources/application.yml` with environment variable substitution. The backend runs on port 80 in both local and Docker environments.

## Key Implementation Details

### MenuCrawler (crawl/MenuCrawler.java)
- Uses JSoup for HTML parsing (CSS selectors defined in config)
- Returns `Optional<Menu>` with robust error handling
- Parses dates and menu items from HTML structure
- Test samples in `src/test/resources/sample-menu-page.html`

### Nginx Reverse Proxy (front/nginx.conf)
- Frontend serves static Svelte build on port 80
- Proxies `/api/*` requests to `foodbox-backend:80`
- Docker networking uses `foodbox-network` bridge

### File-based Database
- Menus stored as files in `./db/` directory (mounted as Docker volume)
- MenuService auto-crawls on startup if data is outdated
- No traditional database server required

### Adding New Menu Sources
1. Update CSS selectors in crawler configuration
2. Update `CRAWL_URL` environment variable
3. Add new sample HTML file to `src/test/resources/`
4. Update tests to validate new HTML structure

## Development Workflow

When modifying the crawler:
1. Save sample HTML from new vendor to `src/test/resources/`
2. Update CSS selectors in `CrawlConfig` or `MenuCrawler`
3. Run `./gradlew test --tests MenuCrawlerTest` to validate
4. Rebuild with `./gradlew clean build`
5. Restart Docker services if deployed

When modifying the frontend:
1. Make changes in `front/src/`
2. Test locally with `npm run dev`
3. Build with `npm run build`
4. Rebuild Docker image: `cd front && docker build -t foodbox-frontend .`

## API Endpoints

- `GET /api/menu/today` - Today's menu
- `GET /api/menu` - All available menus
- `POST /api/menu/crawl` - Manually trigger crawling
- `POST /slack/notify` - Trigger Slack notification

## Comment Guidelines

- Write production code without Korean comments; remove non-essential remarks rather than translating them.
- Prefer expressing intent through clear method or variable names instead of inline comments.
- If a comment is unavoidable (for example, in tests to explain fixtures or assertions), write it in concise English.

## Git Commit Policy & Convention
**NEVER commit changes automatically or proactively.**
- Only commit when the user explicitly asks for a commit with clear instructions like "커밋해줘", "commit this", "create a commit", etc.
- Do not commit after completing tasks, even if the work is finished
- Do not suggest committing unless specifically asked
- Let the user decide when and what to commit
- **When creating commit messages, analyze only the actual code changes since the last commit, not the conversation history.** The commit message should reflect the final code state and changes, not the iterative development process discussed in chat.
- This rule is ABSOLUTE and must NEVER be violated
- **Format:** `type: summary`
    - `type` must be lowercase and chosen from the observed set `{feat, fix, chore, refactor}`. Use `chore` (not `chores`) for maintenance work. Prefer `docs`, `test`, `build`, or `ci` when more specific categories apply.
    - `summary` is a concise, imperative English description (e.g., `fix: ensure duty modal opens on mobile`). Avoid sentence casing, trailing periods, or mixed languages.
- **Body:** add a blank line after the summary if more context is required. Wrap at ~72 chars per line. Mention issue IDs only when relevant.
- **Verification:** always run `git log --oneline -10` before committing to confirm the new message aligns with recent history. Reword (`git commit --amend`) if it deviates.
