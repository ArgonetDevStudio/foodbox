package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const processHelperEnvironment = "FOODBOX_PROCESS_ACCEPTANCE_HELPER"

// TestFoodboxProcessHelper runs the production entry point in a child process.
// The parent test selects this test alone and supplies an isolated environment.
func TestFoodboxProcessHelper(t *testing.T) {
	if os.Getenv(processHelperEnvironment) != "1" {
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "foodbox process helper: %v\n", err)
		os.Exit(2)
	}
}

func TestProductionProcessLoadsEnvironmentServesAndShutsDown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signal acceptance requires SIGTERM")
	}

	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load Seoul location: %v", err)
	}
	today := time.Now().In(location)
	tomorrow := today.AddDate(0, 0, 1)

	dataDirectory := t.TempDir()
	staticDirectory := t.TempDir()
	database := []processDiskMenu{
		processMenu(today, []string{"today soup", "today main", "today side"}),
		processMenu(tomorrow, []string{"tomorrow soup", "tomorrow main", "tomorrow side"}),
	}
	writeProcessJSON(t, filepath.Join(dataDirectory, "db.json"), database)
	index := []byte("<!doctype html><title>foodbox process acceptance</title>")
	if err := os.WriteFile(filepath.Join(staticDirectory, "index.html"), index, 0o600); err != nil {
		t.Fatalf("write static index: %v", err)
	}

	var externalCalls atomic.Int32
	external := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		externalCalls.Add(1)
		http.Error(response, "unexpected external request", http.StatusInternalServerError)
	}))
	t.Cleanup(external.Close)
	certificatePath := filepath.Join(t.TempDir(), "fake-external-ca.pem")
	writeServerCertificate(t, external, certificatePath)

	port := reserveProcessPort(t)
	const clovaSecret = "process-clova-secret"
	const slackToken = "T000/B000/process-slack-secret"
	const adminToken = "process-admin-secret"
	environment := map[string]string{
		processHelperEnvironment: "1",
		"SERVER_PORT":            strconv.Itoa(port),
		"DB_FILE_DIR":            dataDirectory,
		"STATIC_DIR":             staticDirectory,
		"TZ":                     "Asia/Seoul",
		"CRAWL_URL":              "https://vendor.example.test/menu",
		"CLOVA_URL":              external.URL + "/clova",
		"CLOVA_SECRET_KEY":       clovaSecret,
		"SLACK_URL":              external.URL + "/services/",
		"SLACK_TOKEN":            slackToken,
		"SLACK_CHANNEL":          "process-lunch",
		"ADMIN_TOKEN":            adminToken,
		"SSL_CERT_FILE":          certificatePath,
	}

	command := exec.Command(os.Args[0], "-test.run=^TestFoodboxProcessHelper$")
	command.Env = processEnvironment(environment)
	var output synchronizedBuffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatalf("start Foodbox process: %v", err)
	}

	waitResult := make(chan error, 1)
	go func() { waitResult <- command.Wait() }()
	processExited := false
	t.Cleanup(func() {
		if processExited {
			return
		}
		_ = command.Process.Kill()
		select {
		case <-waitResult:
		case <-time.After(2 * time.Second):
		}
	})

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	waitForProcessReady(t, baseURL, waitResult, &output)
	assertProcessResponse(t, baseURL+"/healthz", http.StatusOK,
		`{"status":200,"error":null,"data":{"ready":true}}`+"\n")
	assertProcessResponse(t, baseURL+"/api/menu", http.StatusOK, processMenuResponse(t, database))
	assertProcessResponse(t, baseURL+"/calendar", http.StatusOK, string(index))

	if got := externalCalls.Load(); got != 0 {
		t.Fatalf("startup made %d external requests despite future menu coverage", got)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case err := <-waitResult:
		processExited = true
		if err != nil {
			t.Fatalf("Foodbox process exited with error: %v\n%s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Foodbox process did not shut down after SIGTERM\n%s", output.String())
	}

	logs := output.String()
	if !strings.Contains(logs, `"msg":"foodbox started"`) ||
		!strings.Contains(logs, `"msg":"foodbox shutting down"`) {
		t.Fatalf("process lifecycle logs are incomplete:\n%s", logs)
	}
	for _, secret := range []string{clovaSecret, slackToken, adminToken} {
		if strings.Contains(logs, secret) {
			t.Fatalf("process logs disclosed a configured secret:\n%s", logs)
		}
	}
	if got := externalCalls.Load(); got != 0 {
		t.Fatalf("process lifecycle made %d external requests, want none", got)
	}
	if _, err := http.Get(baseURL + "/healthz"); err == nil {
		t.Fatal("HTTP listener still accepted requests after graceful shutdown")
	}
}

type processDiskMenu struct {
	Date  [3]int   `json:"date"`
	Menus []string `json:"menus"`
	Valid bool     `json:"valid"`
}

type processAPIMenu struct {
	Date    string   `json:"date"`
	Menus   []string `json:"menus"`
	IsValid bool     `json:"isValid"`
}

type processEnvelope struct {
	Status int              `json:"status"`
	Error  any              `json:"error"`
	Data   []processAPIMenu `json:"data"`
}

func processMenu(date time.Time, menus []string) processDiskMenu {
	return processDiskMenu{
		Date:  [3]int{date.Year(), int(date.Month()), date.Day()},
		Menus: menus,
		Valid: len(menus) > 2,
	}
}

func processMenuResponse(t *testing.T, database []processDiskMenu) string {
	t.Helper()
	menus := make([]processAPIMenu, 0, len(database))
	for index := len(database) - 1; index >= 0; index-- {
		menu := database[index]
		menus = append(menus, processAPIMenu{
			Date:    fmt.Sprintf("%04d-%02d-%02d", menu.Date[0], menu.Date[1], menu.Date[2]),
			Menus:   menu.Menus,
			IsValid: menu.Valid,
		})
	}
	encoded, err := json.Marshal(processEnvelope{Status: http.StatusOK, Data: menus})
	if err != nil {
		t.Fatalf("encode expected API response: %v", err)
	}
	return string(encoded) + "\n"
}

func writeProcessJSON(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeServerCertificate(t *testing.T, server *httptest.Server, path string) {
	t.Helper()
	certificate := server.TLS.Certificates[0].Certificate[0]
	contents := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write fake HTTPS CA: %v", err)
	}
}

func reserveProcessPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve process port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release process port: %v", err)
	}
	return port
}

func processEnvironment(overrides map[string]string) []string {
	blocked := make(map[string]struct{}, len(overrides))
	for name := range overrides {
		blocked[name] = struct{}{}
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, found := blocked[name]; !found {
			environment = append(environment, entry)
		}
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func waitForProcessReady(t *testing.T, baseURL string, waitResult <-chan error, output *synchronizedBuffer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	client := &http.Client{Timeout: 100 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case err := <-waitResult:
			t.Fatalf("Foodbox process exited before readiness: %v\n%s", err, output.String())
		default:
		}
		response, err := client.Get(baseURL + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Foodbox process did not become ready\n%s", output.String())
}

func assertProcessResponse(t *testing.T, rawURL string, status int, body string) {
	t.Helper()
	response, err := http.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer response.Body.Close()
	actual, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", rawURL, err)
	}
	if response.StatusCode != status || string(actual) != body {
		t.Fatalf("GET %s = %d %q, want %d %q", rawURL, response.StatusCode, actual, status, body)
	}
}

type synchronizedBuffer struct {
	mutex sync.Mutex
	bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.Buffer.Write(data)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.Buffer.String()
}
