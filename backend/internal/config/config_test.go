package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLoadFromLookup(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		"SERVER_PORT":      "9090",
		"DB_FILE_DIR":      "/data",
		"STATIC_DIR":       "/app/static",
		"CLOVA_URL":        "https://clova.example.test/ocr",
		"CLOVA_SECRET_KEY": "clova-secret",
		"SLACK_TOKEN":      "slack-secret",
		"SLACK_CHANNEL":    "lunch",
		"ADMIN_TOKEN":      "admin-secret",
	}
	config, err := LoadFromLookup(mapLookup(environment))
	if err != nil {
		t.Fatalf("LoadFromLookup() error = %v", err)
	}

	if config.Port != 9090 || config.DBFileDir != "/data" || config.StaticDir != "/app/static" {
		t.Fatalf("unexpected deployment config: %+v", config)
	}
	if config.SlackChannel != "#lunch" {
		t.Fatalf("SlackChannel = %q, want #lunch", config.SlackChannel)
	}
	if config.Location.String() != defaultTimeZone {
		t.Fatalf("Location = %q, want %q", config.Location, defaultTimeZone)
	}
	if config.ClovaSecret.Value() != "clova-secret" || config.SlackToken.Value() != "slack-secret" {
		t.Fatal("secret values were not loaded")
	}
	if config.AdminToken.Value() != "admin-secret" {
		t.Fatal("ADMIN_TOKEN was not loaded")
	}
	if strings.Contains(config.String(), "admin-secret") {
		t.Fatal("Config.String() disclosed ADMIN_TOKEN")
	}
}

func TestAdminTokenIsOptional(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		"CLOVA_URL":        "https://clova.example.test/ocr",
		"CLOVA_SECRET_KEY": "clova-secret",
		"SLACK_TOKEN":      "slack-secret",
		"SLACK_CHANNEL":    "#lunch",
	}
	loaded, err := LoadFromLookup(mapLookup(environment))
	if err != nil {
		t.Fatalf("LoadFromLookup() error = %v", err)
	}
	if loaded.AdminToken.Value() != "" {
		t.Fatalf("AdminToken = %q, want disabled empty value", loaded.AdminToken.Value())
	}
}

func TestConfigRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"CLOVA_URL":        "https://clova.example.test/ocr",
		"CLOVA_SECRET_KEY": "clova-secret",
		"SLACK_TOKEN":      "slack-secret",
		"SLACK_CHANNEL":    "#lunch",
	}
	tests := []struct {
		name   string
		key    string
		value  string
		remove string
	}{
		{name: "bad port", key: "SERVER_PORT", value: "70000"},
		{name: "bad Clova URL", key: "CLOVA_URL", value: "not-a-url"},
		{name: "bad timezone", key: "TZ", value: "Nowhere/Invalid"},
		{name: "missing secret", remove: "CLOVA_SECRET_KEY"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			environment := cloneMap(valid)
			if test.key != "" {
				environment[test.key] = test.value
			}
			delete(environment, test.remove)
			if _, err := LoadFromLookup(mapLookup(environment)); err == nil {
				t.Fatal("LoadFromLookup() unexpectedly succeeded")
			}
		})
	}
}

func TestSecretsAreRedacted(t *testing.T) {
	t.Parallel()

	const credential = "must-never-appear"
	secret := NewSecret(credential)
	outputs := []string{
		fmt.Sprint(secret),
		fmt.Sprintf("%#v", secret),
		fmt.Sprintf("%+v", secret),
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	outputs = append(outputs, string(encoded))

	for _, output := range outputs {
		if strings.Contains(output, credential) {
			t.Fatalf("secret leaked through formatting: %s", output)
		}
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func cloneMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
