package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/clova"
	"github.com/LooLookProject/foodbox/backend/internal/crawler"
	"github.com/LooLookProject/foodbox/backend/internal/domain"
	"github.com/LooLookProject/foodbox/backend/internal/httpapi"
	"github.com/LooLookProject/foodbox/backend/internal/ocr"
	"github.com/LooLookProject/foodbox/backend/internal/service"
	"github.com/LooLookProject/foodbox/backend/internal/slack"
	"github.com/LooLookProject/foodbox/backend/internal/store"
)

const acceptanceAdminToken = "acceptance-admin-token"

type acceptanceGoldenMenu struct {
	Date  string   `json:"date"`
	Menus []string `json:"menus"`
	Valid bool     `json:"valid"`
}

type acceptanceDiskMenu struct {
	Date  [3]int   `json:"date"`
	Menus []string `json:"menus"`
	Valid bool     `json:"valid"`
}

type acceptanceAPIMenu struct {
	Date    string   `json:"date"`
	Menus   []string `json:"menus"`
	IsValid bool     `json:"isValid"`
}

type acceptanceEnvelope struct {
	Status int                 `json:"status"`
	Error  any                 `json:"error"`
	Data   []acceptanceAPIMenu `json:"data"`
}

type acceptanceClovaRequest struct {
	Method      string
	Path        string
	Secret      string
	ContentType string
	ImageFormat string
	ImageHash   [32]byte
	DecodeError error
}

type acceptanceSlackRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

type acceptanceDependencies struct {
	crawler *crawler.Client
	clova   *clova.Client
	slack   *slack.Client
}

func TestLegacyMigrationAcceptanceLifecycle(t *testing.T) {
	fixtureDirectory := filepath.Join("..", "..", "internal", "ocr", "testdata")
	image := acceptanceReadFile(t, filepath.Join(fixtureDirectory, "eiso_202607.jpg"))
	clovaResponse := acceptanceReadFile(t, filepath.Join(fixtureDirectory, "eiso_202607.json"))
	var golden []acceptanceGoldenMenu
	acceptanceDecodeJSON(t, acceptanceReadFile(t, filepath.Join(fixtureDirectory, "eiso_202607.golden.json")), &golden)

	dataDirectory := t.TempDir()
	legacyDatabase := []byte(`[{"date":[2026,5,1],"menus":["legacy soup","legacy main","legacy side"],"valid":true}]`)
	acceptanceWriteFile(t, filepath.Join(dataDirectory, "db.json"), legacyDatabase)
	staticDirectory := t.TempDir()
	acceptanceWriteFile(t, filepath.Join(staticDirectory, "index.html"), []byte("<!doctype html><title>foodbox acceptance</title>"))

	var serveChangedImage atomic.Bool
	var vendorRequests atomic.Int32
	vendor := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		vendorRequests.Add(1)
		switch request.URL.Path {
		case "/board":
			month := time.Now().In(time.FixedZone("Asia/Seoul", 9*60*60)).Format("2006년 01월")
			_, _ = fmt.Fprintf(response, `<div class="bbs-list"><a class="aline" href="/detail">%s 식단표</a></div>`, month)
		case "/detail":
			_, _ = io.WriteString(response, `<div id="bo_v_con"><img src="/menu.jpg"></div>`)
		case "/menu.jpg":
			response.Header().Set("Content-Type", "image/jpeg")
			_, _ = response.Write(image)
			if serveChangedImage.Load() {
				_, _ = response.Write([]byte{0})
			}
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(vendor.Close)

	clovaRequests := make(chan acceptanceClovaRequest, 4)
	var clovaCalls atomic.Int32
	var failClova atomic.Bool
	clovaServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		clovaCalls.Add(1)
		event := acceptanceDecodeClovaRequest(request)
		clovaRequests <- event
		if failClova.Load() {
			http.Error(response, "mock OCR failure", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(clovaResponse)
	}))
	t.Cleanup(clovaServer.Close)

	slackRequests := make(chan acceptanceSlackRequest, 2)
	slackServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		slackRequests <- acceptanceSlackRequest{
			Method:      request.Method,
			Path:        request.URL.Path,
			ContentType: request.Header.Get("Content-Type"),
			Body:        body,
		}
		_, _ = io.WriteString(response, "ok")
	}))
	t.Cleanup(slackServer.Close)

	crawlClient, err := crawler.NewClient(
		"https://vendor.example.test/board",
		crawler.WithHTTPClient(acceptanceHTTPSClient(vendor)),
	)
	if err != nil {
		t.Fatalf("create crawler: %v", err)
	}
	clovaClient, err := clova.NewClient(
		clovaServer.URL+"/invoke",
		"acceptance-clova-secret",
		clova.WithHTTPClient(clovaServer.Client()),
	)
	if err != nil {
		t.Fatalf("create Clova client: %v", err)
	}
	slackClient, err := slack.NewClient(
		slackServer.URL+"/services",
		"T000/B000/acceptance-secret",
		slack.WithHTTPClient(slackServer.Client()),
	)
	if err != nil {
		t.Fatalf("create Slack client: %v", err)
	}
	dependencies := acceptanceDependencies{crawler: crawlClient, clova: clovaClient, slack: slackClient}
	fixedDate := domain.LocalDate{Year: 2026, Month: 7, Day: 23}

	application := acceptanceStartApplication(t, dataDirectory, staticDirectory, fixedDate, dependencies)

	response := acceptanceRequest(t, application.URL, http.MethodGet, "/healthz", "")
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":{"ready":true}}`+"\n")

	response = acceptanceRequest(t, application.URL, http.MethodGet, "/api/menu", "")
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":[{"date":"2026-05-01","menus":["legacy soup","legacy main","legacy side"],"isValid":true}]}`+"\n")

	response = acceptanceRequest(t, application.URL, http.MethodGet, "/calendar", "")
	acceptanceAssertResponse(t, response, http.StatusOK, "<!doctype html><title>foodbox acceptance</title>")

	response = acceptanceRequest(t, application.URL, http.MethodPost, "/api/crawl", acceptanceAdminToken)
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":"ok"}`+"\n")
	if got := vendorRequests.Load(); got != 3 {
		t.Fatalf("vendor requests after crawl = %d, want 3", got)
	}
	firstClovaRequest := acceptanceReceive(t, clovaRequests, "Clova request")
	if firstClovaRequest.DecodeError != nil {
		t.Fatalf("decode Clova request: %v", firstClovaRequest.DecodeError)
	}
	if firstClovaRequest.Method != http.MethodPost || firstClovaRequest.Path != "/invoke" ||
		firstClovaRequest.Secret != "acceptance-clova-secret" || firstClovaRequest.ContentType != "application/json" ||
		firstClovaRequest.ImageFormat != "png" || firstClovaRequest.ImageHash != sha256.Sum256(image) {
		t.Fatalf("unexpected Clova request: %+v", firstClovaRequest)
	}

	expectedDisk, expectedAPI := acceptanceExpectedMenus(t, golden)
	actualDisk := acceptanceReadFile(t, filepath.Join(dataDirectory, "db.json"))
	if !bytes.Equal(actualDisk, expectedDisk) {
		t.Fatalf("database differs from legacy format\ngot:  %s\nwant: %s", actualDisk, expectedDisk)
	}

	response = acceptanceRequest(t, application.URL, http.MethodGet, "/api/menu", "")
	acceptanceAssertResponse(t, response, http.StatusOK, string(expectedAPI))

	response = acceptanceRequest(t, application.URL, http.MethodGet, "/api/menu/today", "")
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":{"date":"2026-07-23","menus":["된장찌개","제육볶음","무생채나물무침","행야채볶음","김말이튀김","산고추무침","꼬마김치"],"isValid":true}}`+"\n")

	response = acceptanceRequest(t, application.URL, http.MethodPost, "/api/slack/notify", acceptanceAdminToken)
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":true}`+"\n")
	slackRequest := acceptanceReceive(t, slackRequests, "Slack request")
	wantSlackBody := []byte(`{"channel":"#lunch","username":"점심봇","text":"\u003c2026-07-23 목요일\u003e 점심 메뉴\n• 된장찌개\n• 제육볶음\n• 무생채나물무침\n• 행야채볶음\n• 김말이튀김\n• 산고추무침\n• 꼬마김치","icon_emoji":":bento:"}`)
	if slackRequest.Method != http.MethodPost || slackRequest.Path != "/services/T000/B000/acceptance-secret" ||
		slackRequest.ContentType != "application/json" || !bytes.Equal(slackRequest.Body, wantSlackBody) {
		t.Fatalf("unexpected Slack request: method=%s path=%s content-type=%s\ngot:  %s\nwant: %s", slackRequest.Method, slackRequest.Path, slackRequest.ContentType, slackRequest.Body, wantSlackBody)
	}

	metadataPath := filepath.Join(dataDirectory, "metadata.json")
	imageHash := fmt.Sprintf("%x", md5.Sum(image))
	wantMetadata := []byte("{\n  \"lastImageHash\": \"" + imageHash + "\"\n}\n")
	if actual := acceptanceReadFile(t, metadataPath); !bytes.Equal(actual, wantMetadata) {
		t.Fatalf("metadata after crawl = %s, want %s", actual, wantMetadata)
	}

	application.Close()
	restarted := acceptanceStartApplication(t, dataDirectory, staticDirectory, fixedDate, dependencies)
	t.Cleanup(restarted.Close)

	response = acceptanceRequest(t, restarted.URL, http.MethodGet, "/healthz", "")
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":{"ready":true}}`+"\n")
	response = acceptanceRequest(t, restarted.URL, http.MethodGet, "/api/menu", "")
	acceptanceAssertResponse(t, response, http.StatusOK, string(expectedAPI))

	clovaCallsBeforeDuplicate := clovaCalls.Load()
	vendorRequestsBeforeDuplicate := vendorRequests.Load()
	response = acceptanceRequest(t, restarted.URL, http.MethodPost, "/api/crawl", acceptanceAdminToken)
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":"ok"}`+"\n")
	if got := clovaCalls.Load(); got != clovaCallsBeforeDuplicate {
		t.Fatalf("Clova calls after restart duplicate = %d, want %d", got, clovaCallsBeforeDuplicate)
	}
	if got := vendorRequests.Load(); got != vendorRequestsBeforeDuplicate+3 {
		t.Fatalf("vendor requests after restart duplicate = %d, want %d", got, vendorRequestsBeforeDuplicate+3)
	}
	if actual := acceptanceReadFile(t, filepath.Join(dataDirectory, "db.json")); !bytes.Equal(actual, expectedDisk) {
		t.Fatal("duplicate crawl after restart changed the database")
	}

	databaseBeforeFailure := acceptanceReadFile(t, filepath.Join(dataDirectory, "db.json"))
	metadataBeforeFailure := acceptanceReadFile(t, metadataPath)
	serveChangedImage.Store(true)
	failClova.Store(true)
	response = acceptanceRequest(t, restarted.URL, http.MethodPost, "/api/crawl", acceptanceAdminToken)
	acceptanceAssertResponse(t, response, http.StatusInternalServerError, `{"status":500,"error":{"errorCode":"INTERNAL_SERVER_ERROR","message":"internal server error"},"data":null}`+"\n")
	failedClovaRequest := acceptanceReceive(t, clovaRequests, "failed Clova request")
	if failedClovaRequest.DecodeError != nil {
		t.Fatalf("decode failed Clova request: %v", failedClovaRequest.DecodeError)
	}
	changedImage := append(append([]byte(nil), image...), 0)
	if failedClovaRequest.ImageHash != sha256.Sum256(changedImage) {
		t.Fatal("failure path did not submit the changed vendor image to Clova")
	}
	if actual := acceptanceReadFile(t, filepath.Join(dataDirectory, "db.json")); !bytes.Equal(actual, databaseBeforeFailure) {
		t.Fatal("failed OCR crawl changed the database")
	}
	if actual := acceptanceReadFile(t, metadataPath); !bytes.Equal(actual, metadataBeforeFailure) {
		t.Fatal("failed OCR crawl changed the persisted image hash")
	}
	response = acceptanceRequest(t, restarted.URL, http.MethodGet, "/healthz", "")
	acceptanceAssertResponse(t, response, http.StatusOK, `{"status":200,"error":null,"data":{"ready":true}}`+"\n")
}

func acceptanceHTTPSClient(server *httptest.Server) *http.Client {
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	transport.TLSClientConfig = &tls.Config{ //nolint:gosec -- isolated httptest server with a mapped public hostname
		InsecureSkipVerify: true,
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Second}
}

func acceptanceStartApplication(
	t *testing.T,
	dataDirectory string,
	staticDirectory string,
	today domain.LocalDate,
	dependencies acceptanceDependencies,
) *httptest.Server {
	t.Helper()
	repository, err := store.NewFileStore(dataDirectory)
	if err != nil {
		t.Fatalf("open menu store: %v", err)
	}
	hashes, err := store.NewHashStore(dataDirectory)
	if err != nil {
		t.Fatalf("open hash store: %v", err)
	}
	menus := service.NewMenuService(
		repository,
		dependencies.crawler,
		clovaAdapter{client: dependencies.clova},
		service.ParserFunc(ocr.Parse),
		hashes,
		service.WithDateClock(func() domain.LocalDate { return today }),
	)
	notifications := service.NewNotificationService(
		menus,
		slackAdapter{client: dependencies.slack},
		service.NotificationConfig{Channel: "lunch", Username: "점심봇", IconEmoji: ":bento:"},
		service.WithNotificationDateClock(func() domain.LocalDate { return today }),
	)
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	handler, err := httpapi.NewHandler(httpapi.Config{
		AdminToken: acceptanceAdminToken,
		StaticDir:  staticDirectory,
		Location:   location,
		Now: func() time.Time {
			return time.Date(today.Year, time.Month(today.Month), today.Day, 12, 0, 0, 0, location)
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, menus, notifications)
	if err != nil {
		t.Fatalf("build HTTP handler: %v", err)
	}
	return httptest.NewServer(handler)
}

func acceptanceDecodeClovaRequest(request *http.Request) acceptanceClovaRequest {
	event := acceptanceClovaRequest{
		Method:      request.Method,
		Path:        request.URL.Path,
		Secret:      request.Header.Get("X-OCR-SECRET"),
		ContentType: request.Header.Get("Content-Type"),
	}
	var body struct {
		Images []struct {
			Format string `json:"format"`
			Data   string `json:"data"`
		} `json:"images"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		event.DecodeError = err
		return event
	}
	if len(body.Images) != 1 {
		event.DecodeError = fmt.Errorf("image count = %d, want 1", len(body.Images))
		return event
	}
	image, err := base64.StdEncoding.DecodeString(body.Images[0].Data)
	if err != nil {
		event.DecodeError = err
		return event
	}
	event.ImageFormat = body.Images[0].Format
	event.ImageHash = sha256.Sum256(image)
	return event
}

func acceptanceExpectedMenus(t *testing.T, golden []acceptanceGoldenMenu) ([]byte, []byte) {
	t.Helper()
	disk := make([]acceptanceDiskMenu, 0, len(golden)+1)
	disk = append(disk, acceptanceDiskMenu{
		Date:  [3]int{2026, 5, 1},
		Menus: []string{"legacy soup", "legacy main", "legacy side"},
		Valid: true,
	})
	for _, menu := range golden {
		disk = append(disk, acceptanceDiskMenu{
			Date:  acceptanceDateArray(t, menu.Date),
			Menus: menu.Menus,
			Valid: menu.Valid,
		})
	}
	diskJSON, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		t.Fatalf("encode expected database: %v", err)
	}
	diskJSON = append(diskJSON, '\n')

	apiMenus := make([]acceptanceAPIMenu, 0, len(disk))
	for index := len(golden) - 1; index >= 0; index-- {
		apiMenus = append(apiMenus, acceptanceAPIMenu{
			Date:    golden[index].Date,
			Menus:   golden[index].Menus,
			IsValid: golden[index].Valid,
		})
	}
	apiMenus = append(apiMenus, acceptanceAPIMenu{
		Date:    "2026-05-01",
		Menus:   []string{"legacy soup", "legacy main", "legacy side"},
		IsValid: true,
	})
	apiJSON, err := json.Marshal(acceptanceEnvelope{Status: http.StatusOK, Data: apiMenus})
	if err != nil {
		t.Fatalf("encode expected API response: %v", err)
	}
	apiJSON = append(apiJSON, '\n')
	return diskJSON, apiJSON
}

func acceptanceDateArray(t *testing.T, value string) [3]int {
	t.Helper()
	parts := strings.Split(value, "-")
	if len(parts) != 3 {
		t.Fatalf("invalid golden date %q", value)
	}
	var result [3]int
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			t.Fatalf("invalid golden date %q: %v", value, err)
		}
		result[index] = number
	}
	return result
}

func acceptanceRequest(t *testing.T, baseURL, method, path, adminToken string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, nil)
	if err != nil {
		t.Fatalf("create %s %s request: %v", method, path, err)
	}
	if adminToken != "" {
		request.Header.Set("Authorization", "Bearer "+adminToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform %s %s request: %v", method, path, err)
	}
	return response
}

func acceptanceAssertResponse(t *testing.T, response *http.Response, status int, body string) {
	t.Helper()
	defer response.Body.Close()
	actualBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read HTTP response: %v", err)
	}
	if response.StatusCode != status || string(actualBody) != body {
		t.Fatalf("HTTP response = %d %q, want %d %q", response.StatusCode, actualBody, status, body)
	}
}

func acceptanceReceive[T any](t *testing.T, channel <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func acceptanceReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func acceptanceWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func acceptanceDecodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}
