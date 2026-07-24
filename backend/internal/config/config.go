package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort       = 8080
	defaultDBDir      = "./db"
	defaultStaticDir  = "./static"
	defaultSlackURL   = "https://hooks.slack.com/services/"
	defaultSlackUser  = "점심봇"
	defaultTimeZone   = "Asia/Seoul"
	defaultCrawlerURL = "https://eisodosirak.itpage.kr/bbs/board.php?bo_table=basic4"
)

// Secret prevents credentials from being disclosed through common formatting
// and JSON logging. Value must only be called at the integration boundary.
type Secret struct {
	value string
}

func NewSecret(value string) Secret {
	return Secret{value: value}
}

func (s Secret) Value() string {
	return s.value
}

func (Secret) String() string {
	return "[REDACTED]"
}

func (Secret) GoString() string {
	return "config.Secret{[REDACTED]}"
}

func (Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal("[REDACTED]")
}

type Config struct {
	Port          int
	DBFileDir     string
	StaticDir     string
	CrawlURL      string
	ClovaURL      string
	ClovaSecret   Secret
	SlackURL      string
	SlackToken    Secret
	SlackChannel  string
	SlackUsername string
	AdminToken    Secret
	Location      *time.Location
}

// Load reads configuration exclusively from the process environment.
func Load() (*Config, error) {
	return LoadFromLookup(os.LookupEnv)
}

// LoadFromLookup makes environment parsing deterministic in tests without
// allowing a second configuration source in production.
func LoadFromLookup(lookup func(string) (string, bool)) (*Config, error) {
	if lookup == nil {
		return nil, errors.New("configuration environment lookup is nil")
	}

	port, err := parsePort(valueOrDefault(lookup, "SERVER_PORT", strconv.Itoa(defaultPort)))
	if err != nil {
		return nil, err
	}

	zoneName := valueOrDefault(lookup, "TZ", defaultTimeZone)
	location, err := time.LoadLocation(zoneName)
	if err != nil {
		return nil, fmt.Errorf("load TZ %q: %w", zoneName, err)
	}

	clovaURL, err := required(lookup, "CLOVA_URL")
	if err != nil {
		return nil, err
	}
	clovaSecret, err := required(lookup, "CLOVA_SECRET_KEY")
	if err != nil {
		return nil, err
	}
	slackToken, err := required(lookup, "SLACK_TOKEN")
	if err != nil {
		return nil, err
	}
	slackChannel, err := required(lookup, "SLACK_CHANNEL")
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(slackChannel, "#") {
		slackChannel = "#" + slackChannel
	}

	crawlURL := valueOrDefault(lookup, "CRAWL_URL", defaultCrawlerURL)
	slackURL := valueOrDefault(lookup, "SLACK_URL", defaultSlackURL)
	if !strings.HasSuffix(slackURL, "/") {
		slackURL += "/"
	}
	for name, rawURL := range map[string]string{
		"CLOVA_URL": clovaURL,
		"CRAWL_URL": crawlURL,
		"SLACK_URL": slackURL,
	} {
		if err := validateHTTPURL(name, rawURL); err != nil {
			return nil, err
		}
	}

	dbDir, err := cleanDirectory("DB_FILE_DIR", valueOrDefault(lookup, "DB_FILE_DIR", defaultDBDir))
	if err != nil {
		return nil, err
	}
	staticDir, err := cleanDirectory("STATIC_DIR", valueOrDefault(lookup, "STATIC_DIR", defaultStaticDir))
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:          port,
		DBFileDir:     dbDir,
		StaticDir:     staticDir,
		CrawlURL:      crawlURL,
		ClovaURL:      clovaURL,
		ClovaSecret:   NewSecret(clovaSecret),
		SlackURL:      slackURL,
		SlackToken:    NewSecret(slackToken),
		SlackChannel:  slackChannel,
		SlackUsername: valueOrDefault(lookup, "SLACK_USERNAME", defaultSlackUser),
		AdminToken:    NewSecret(valueOrDefault(lookup, "ADMIN_TOKEN", "")),
		Location:      location,
	}, nil
}

func (c Config) String() string {
	zone := "<nil>"
	if c.Location != nil {
		zone = c.Location.String()
	}
	return fmt.Sprintf(
		"Config{Port:%d DBFileDir:%q StaticDir:%q CrawlURL:%q ClovaURL:%q ClovaSecret:%s SlackURL:%q SlackToken:%s SlackChannel:%q SlackUsername:%q AdminToken:%s Location:%q}",
		c.Port,
		c.DBFileDir,
		c.StaticDir,
		c.CrawlURL,
		c.ClovaURL,
		c.ClovaSecret,
		c.SlackURL,
		c.SlackToken,
		c.SlackChannel,
		c.SlackUsername,
		c.AdminToken,
		zone,
	)
}

func valueOrDefault(lookup func(string) (string, bool), name, fallback string) string {
	if value, ok := lookup(name); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func required(lookup func(string) (string, bool), name string) (string, error) {
	value, ok := lookup(name)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", fmt.Errorf("required environment variable %s is not set", name)
	}
	return value, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("SERVER_PORT must be an integer between 1 and 65535: %q", value)
	}
	return port, nil
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", name)
	}
	return nil
}

func cleanDirectory(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." && value != "." && value != "./" {
		return "", fmt.Errorf("%s must not resolve to an empty path", name)
	}
	return cleaned, nil
}
