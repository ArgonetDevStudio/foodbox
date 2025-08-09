# 🍽️ Foodbox

## Overview

### Preview

![preview](README.assets/preview.png)

### Intro

Foodbox is a Spring Boot application designed to make it easy for employees to check the daily lunch menu. The lunch vendor posts the menu on their website as HTML, and this project automates the process of crawling and sharing that information with everyone. The key features include:

- **Web Crawling**: Fetching the daily menu from the food vendor's website using JSoup HTML parsing
- **Data Structuring**: Analyzing and organizing the menu for each date into a structured format
- **REST API**: Providing HTTP endpoints to access menu information
- **Slack Integration**: Sending the lunch menu via a Slack bot at the beginning of each workday
- **Automatic Scheduling**: Daily notifications to ensure everyone knows what's for lunch

The goal is to ensure everyone has quick and easy access to the lunch menu, without needing to search for it manually.

## Architecture

### Technology Stack

- **Backend**: Spring Boot 3.3.5 (Java 21)
- **Web Crawling**: JSoup 1.15.3
- **Database**: File-based storage with Spring Data
- **Messaging**: Slack API integration
- **Testing**: JUnit 5, Mockito, AssertJ
- **Build Tool**: Gradle

### Project Structure

```
src/
├── main/java/shanepark/foodbox/
│   ├── FoodboxApplication.java          # Main application entry point
│   ├── api/
│   │   ├── controller/                  # REST API controllers
│   │   ├── domain/                      # Domain entities (Menu, MenuResponse, etc.)
│   │   ├── service/                     # Business logic services
│   │   ├── repository/                  # Data access layer
│   │   ├── config/                      # Configuration classes
│   │   └── exception/                   # Custom exceptions
│   ├── crawl/
│   │   ├── MenuCrawler.java            # Web crawling logic
│   │   └── CrawlConfig.java            # Crawling configuration
│   └── slack/
│       ├── controller/                  # Slack webhook controllers
│       ├── service/                     # Slack notification services
│       └── domain/                      # Slack-related data structures
└── test/
    ├── resources/
    │   └── sample-menu-page.html       # Test HTML samples
    └── java/shanepark/foodbox/
        └── crawl/
            └── MenuCrawlerTest.java    # Comprehensive crawler tests
```

### Key Components

#### MenuCrawler
- **Purpose**: Crawls menu data from the vendor's website
- **Technology**: JSoup for HTML parsing
- **Features**: 
  - Robust error handling with Optional-based parsing
  - Configurable CSS selectors
  - Date extraction and validation
  - Menu item parsing and structuring

#### MenuService
- **Purpose**: Business logic for menu management
- **Features**:
  - Automatic crawling on startup if data is outdated
  - Weekend handling (no lunch service)
  - REST API integration

#### Slack Integration
- **Purpose**: Automated notifications to team members
- **Features**:
  - Configurable scheduling
  - Custom message formatting
  - Error handling and retry logic

## API Endpoints

### Menu API

- `GET /api/menu/today` - Get today's menu
- `GET /api/menu/all` - Get all available menus
- `POST /api/menu/crawl` - Manually trigger menu crawling

### Slack Notification

- `POST /slack/notify` - Trigger Slack notifications

## Configuration

### Application Properties

The application uses `application.yml` for configuration:

```yaml
spring:
  application:
    name: foodbox

crawl:
  crawl-url: ${CRAWL_URL:http://www.msmfood.co.kr/page/sub2_7}

slack:
  slack-token: ${SLACK_TOKEN:"YOUR_SLACK_TOKEN_HERE"}
  slack-channel: ${SLACK_CHANNEL:"YOUR_SLACK_CHANNEL_HERE"}
  user-name: "점심봇"
  slack-url: "https://hooks.slack.com/services"

foodbox:
  db-file-dir: ${DB_FILE_DIR:"/foodbox/db"}

server:
  port: 80
```

### Environment Variables

Create a `.env` file with the following variables:

```properties
SLACK_TOKEN=your_slack_token_here
SLACK_CHANNEL=#your_slack_channel_here
CRAWL_URL=http://www.msmfood.co.kr/page/sub2_7
DB_FILE_DIR=/path/to/database/directory
```

## Deployment

### Prerequisites

- Java 21+
- Docker (optional)
- Docker Compose (optional)

### Local Development

```bash
git clone https://github.com/ArgonetDevStudio/foodbox.git
cd foodbox
./gradlew clean build
./gradlew bootRun
```

### Docker Deployment

```bash
git clone https://github.com/ArgonetDevStudio/foodbox.git
cd foodbox
./gradlew clean build
docker compose up -d
```

### Testing

Run all tests:
```bash
./gradlew test
```

Run specific test class:
```bash
./gradlew test --tests MenuCrawlerTest
```

## Development

### Adding New Menu Sources

1. Create a new crawler implementation following the `MenuCrawler` pattern
2. Add appropriate CSS selectors for the new website structure
3. Configure the new URL in `application.yml`
4. Add comprehensive tests with sample HTML files

### Extending Slack Integration

1. Modify `SlackNotifyService` for new notification patterns
2. Add new message templates in the Slack domain objects
3. Configure additional webhook endpoints if needed

### Testing Strategy

The project uses comprehensive testing with:
- **Unit Tests**: Mock-based testing for individual components
- **Integration Tests**: File-based HTML samples for realistic crawling tests
- **Test Data**: Sample HTML files in `src/test/resources/`

## Troubleshooting

### Common Issues

1. **Crawling Fails**: Check if the target website structure has changed
2. **Slack Notifications Not Working**: Verify token and channel configuration
3. **Date Parsing Issues**: Check if the website's date format has changed

### Logging

The application provides detailed logging for:
- Crawling operations and results
- Menu parsing and validation
- Slack notification attempts
- Configuration validation on startup

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add comprehensive tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

This project is licensed under the MIT License.