# CLAUDE.md

This file provides guidance to AI Agents when working with code in this repository.

## Project Overview

Foodbox is a lunch menu notification system for Eisodosirak (이소도시락) vendor. It downloads menu images, extracts data via Naver Clova OCR, stores menus in a file-based database, and sends daily Slack notifications with special day handling logic.

**Tech Stack**: Spring Boot (Java 25) + Svelte 5 + Nginx + Docker Compose

## Core Domain Models

### Menu (api/domain/Menu.java)
Core entity representing a single day's menu.

**Fields**:
- `date: LocalDate` - Menu date
- `menus: List<String>` - Menu items
- `isValid: boolean` - Validation flag (true if menus.size() > 2)

**Validation Rule**: A menu is considered valid only if it contains 3+ items. Invalid menus are filtered out during Slack notifications.

### MenuResponse (api/domain/MenuResponse.java)
DTO for API responses. Java record wrapping Menu data with date as String.

### ParsedMenuEiso (image/domain/ParsedMenuEiso.java)
Eisodosirak-specific parsed menu from OCR results.

**Key Logic**:
- Parses Korean date format: `월/일` → `LocalDate`
- Regex pattern: `(\\d{1,2})월\\s*(\\d{1,2})일`
- **Year Inference**: Resolves ambiguous month/day to full date by finding closest date within ±45 days from today
- Handles OCR typos: "윌" → "월"

### NotifyDate (slack/domain/enums/NotifyDate.java)
Enum defining notification behavior by day type.

**Values**:
- `BENTO_DAY` - Regular menu notification (Mon/Tue/Thu/Fri)
- `SALAD_DAY` - "데니스델리 🥗" (first 3 Wednesdays of month)
- `EATING_OUT_DAY` - "외식 🍽" (last Wednesday of month)
- `WEEKENDS` - Skip notification

**Logic**: `of(LocalDate)` determines day type. Wednesday classification uses: `month.length - dayOfMonth >= 7` to detect last week.

## System Architecture

### api/ - REST API Layer

**Controllers**:
- `MenuApiController` - Menu endpoints (today, all, crawl, upload)
- `SlackNotifyController` - Manual Slack notification trigger
- `ErrorControllerAdvice` - Global exception handler

**Services**:
- `MenuService` - Core business logic: crawling, parsing, persistence
  - `@PostConstruct init()` - Auto-crawls on startup if data is outdated
  - `crawl()` - Downloads image, checks MD5 hash to prevent duplicate processing
  - `getTodayMenu()` - Returns weekend message or triggers crawl if menu missing

**Repository**:
- `MenuRepository` - File-based CRUD operations on `./db/` directory

**Configuration**:
- `DbFileConfig` - Database directory path
- `TimeConfig` - Clock bean for testable time operations
- `ObjectMapperConfig` - JSON serialization settings

### crawl/ - Image Downloading

**MenuCrawler**:
- `getMenuImage(CrawlConfig)` - PRIMARY: Downloads menu image from vendor website
- `crawlMenus(CrawlConfig)` - LEGACY: JSoup HTML parsing (not actively used)

### image/ - OCR Processing Pipeline

**OCR Components** (image/ocr/clova/):
- `NaverClovaApi` - HTTP client for Naver Clova OCR API
- `ImageParserClovaEiso` - Parses Clova OCR response into menu data
  - Divides image into day regions using ImageMarginCalculator
  - Extracts date and menu text from each region
  - Returns List<ParsedMenu>
- `ImageMarginCalculatorEiso` - Calculates image region bounds for date/menu sections (Eisodosirak layout-specific)

**Domain Models** (image/domain/):
- `DayRegion` - Bounding box for single day's menu in image
- `ParseRegion` - Generic region definition for OCR parsing
- `ParsedMenu` - Generic parsed menu result

### slack/ - Slack Integration

**SlackNotifyService**:
- `@Scheduled(cron = "0 0 9 * * *")` - Daily 9 AM notification
- **Business Logic**:
  1. Determine day type via `NotifyDate.of(today)`
  2. Skip if weekends
  3. Get today's menu from MenuService
  4. Skip if menu is invalid (holiday detection)
  5. Replace menu with special message for SALAD_DAY/EATING_OUT_DAY
  6. Format message with date + day of week (Korean) + menu items
  7. Send via SlackMessageSender

**SlackMessageSender**:
- Sends POST request to Slack webhook URL with SlackPayload

**SlackConfig**:
- Configuration properties for Slack integration

## API Endpoints

| Method | Endpoint | Purpose | Response |
|--------|----------|---------|----------|
| GET | `/api/menu/today` | Today's menu | `ApiResponse<MenuResponse>` |
| GET | `/api/menu` | All menus | `ApiResponse<List<MenuResponse>>` |
| GET | `/api/crawl` | Trigger crawling | Plain string |
| POST | `/api/upload` | Upload image (max 10MB) | `ApiResponse<List<MenuResponse>>` |
| GET | `/api/slack/notify` | Trigger notification | Plain string |

**Note**: `/api/crawl` and `/api/slack/notify` should use POST for RESTful compliance but currently use GET.

## Configuration

### Environment Variables

Create `.env` file based on `.env.example`:

```bash
# Slack
SLACK_TOKEN=xoxb-your-token
SLACK_CHANNEL=#lunch

# Crawling
CRAWL_URL=https://eisodosirak.itpage.kr/bbs/board.php?bo_table=basic4

# Naver Clova OCR (Required)
CLOVA_URL=https://your-clova-endpoint
CLOVA_SECRET_KEY=your-base64-secret-key

# Database (Optional)
DB_FILE_DIR=/path/to/db  # Default: ./db (local), /foodbox/db (Docker)
```

### Application Profiles

**application.yml** (Production - Docker):
- Server port: 80
- Multipart max file size: 10MB

**application-dev.yml** (Development):
- Server port: 8080
- Database: `/tmp/foodbox/db`

## Development Guide

### Build & Run

```bash
# Backend
./gradlew clean build
./gradlew bootRun              # Runs on port 8080 (dev profile)
./gradlew test

# Frontend
cd front
npm install
npm run dev                    # Runs on port 5173, proxies API to localhost:8080
npm run build

# Docker
docker compose up -d           # Frontend: 80/443, Backend: 80
docker compose logs -f foodbox-backend
```

### Testing

**Test Classes** (35 total source files):
- `MenuRepositoryTest` - File persistence
- `ImageMarginCalculatorEisoTest` - Region detection
- `ImageParserClovaEisoTest` - OCR parsing, date inference
- `SlackNotifyServiceTest` - 13 test cases covering all day types
- `SlackMessageSenderTest` - Webhook integration

**Test Resources**:
- `eiso_202510.jpg` - Sample menu image
- `eiso_202510.json` - Sample Clova OCR response

### Workflow: Modifying Image Processing

1. Save sample image to `src/test/resources/`
2. Update region calculation in `ImageMarginCalculatorEiso`
3. Update parsing logic in `ImageParserClovaEiso`
4. Run `./gradlew test --tests ImageParserClovaEisoTest`
5. Rebuild: `./gradlew clean build`
6. Restart Docker: `docker compose restart foodbox-backend`

### Workflow: Adding New Vendor

1. Create `ImageParserClova{VendorName}` extending `ImageParserClova`
2. Create `ImageMarginCalculator{VendorName}` implementing interface
3. Add test image and OCR JSON to `src/test/resources/`
4. Update `CRAWL_URL` in `.env`
5. Update `MenuCrawler.getMenuImage()` if HTML structure differs
6. Write test class validating OCR parsing

### Workflow: Modifying Slack Logic

1. Edit `SlackNotifyService` or `NotifyDate` enum
2. Add test cases to `SlackNotifyServiceTest`
3. Run `./gradlew test --tests SlackNotifyServiceTest`
4. Deploy

## Key Business Logic Details

### Menu Validation
- **Location**: `Menu.java:24`
- **Rule**: `isValid = menus.size() > 2`
- **Usage**: Slack notifications skip invalid menus (holiday detection)

### Wednesday Special Day Detection
- **Location**: `NotifyDate.java:31`
- **Logic**: Last Wednesday = `month.length - dayOfMonth < 7`
  - First 3 Wednesdays → SALAD_DAY
  - Last Wednesday → EATING_OUT_DAY
- **Test Coverage**: `SlackNotifyServiceTest` includes test for Wednesday falling on holiday (invalid menu skips notification)

### Year Inference for Ambiguous Dates
- **Location**: `ParsedMenuEiso.java:37-64`
- **Problem**: OCR returns "10월 23일" without year
- **Solution**: Try current year, previous year, next year; select date closest to today
- **Constraint**: If closest candidate > 45 days away, fallback to year calculation

### Duplicate Crawl Prevention
- **Location**: `MenuService.java:81-86`
- **Method**: MD5 hash of downloaded image stored in `lastImageHash`
- **Behavior**: Skip parsing if hash matches previous crawl (vendor hasn't uploaded new menu yet)

## Comment Guidelines

- Write code without Korean comments
- Prefer self-documenting method/variable names over inline comments
- If comments are necessary (tests, complex logic), use concise English

## Git Commit Convention

**NEVER commit automatically or proactively.**

Only commit when user explicitly asks with phrases like "커밋해줘", "commit this", "create a commit".

**Format**: `type: summary`

- **type**: `feat`, `fix`, `chore`, `refactor`, `test`, `docs`, `build`, `ci` (lowercase)
- **summary**: Imperative mood, no trailing period (e.g., "add Wednesday holiday detection")

**Process**:
1. Run `git log --oneline -10` to check recent commit style
2. Analyze actual code changes (not conversation history)
3. Write commit message matching project style
4. Run `git status` and `git diff` before committing
5. Commit with heredoc format for proper multiline messages

**Example**:
```bash
git commit -m "$(cat <<'EOF'
feat: add Wednesday holiday notification test

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```
