package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

type fakeMenuService struct {
	readyFn        func(context.Context) error
	findAllFn      func(context.Context) ([]domain.Menu, error)
	todayFn        func(context.Context, domain.LocalDate) (domain.Menu, error)
	crawlFn        func(context.Context) error
	parseAndSaveFn func(context.Context, string) ([]domain.Menu, error)
	saveManualFn   func(context.Context, domain.LocalDate, []string) (domain.Menu, error)
}

func (service *fakeMenuService) Ready(ctx context.Context) error {
	if service.readyFn != nil {
		return service.readyFn(ctx)
	}
	return nil
}

func (service *fakeMenuService) FindAll(ctx context.Context) ([]domain.Menu, error) {
	if service.findAllFn != nil {
		return service.findAllFn(ctx)
	}
	return []domain.Menu{}, nil
}

func (service *fakeMenuService) Today(ctx context.Context, date domain.LocalDate) (domain.Menu, error) {
	if service.todayFn != nil {
		return service.todayFn(ctx, date)
	}
	return domain.NewMenu(date, []string{"menu one", "menu two", "menu three"}), nil
}

func (service *fakeMenuService) Crawl(ctx context.Context) error {
	if service.crawlFn != nil {
		return service.crawlFn(ctx)
	}
	return nil
}

func (service *fakeMenuService) ParseAndSave(ctx context.Context, path string) ([]domain.Menu, error) {
	if service.parseAndSaveFn != nil {
		return service.parseAndSaveFn(ctx, path)
	}
	return []domain.Menu{}, nil
}

func (service *fakeMenuService) SaveManual(ctx context.Context, date domain.LocalDate, menus []string) (domain.Menu, error) {
	if service.saveManualFn != nil {
		return service.saveManualFn(ctx, date, menus)
	}
	return domain.NewMenu(date, menus), nil
}

type fakeNotificationService struct {
	notifyFn func(context.Context) error
}

func (service *fakeNotificationService) NotifyToday(ctx context.Context) error {
	if service.notifyFn != nil {
		return service.notifyFn(ctx)
	}
	return nil
}

func TestMenuEndpointsUseLegacyEnvelopeAndAPIDateFormat(t *testing.T) {
	date, err := domain.NewLocalDate(2026, 7, 25)
	if err != nil {
		t.Fatal(err)
	}
	menus := &fakeMenuService{
		findAllFn: func(context.Context) ([]domain.Menu, error) {
			return []domain.Menu{domain.NewMenu(date, []string{"a", "b", "c"})}, nil
		},
	}
	handler := newTestHandler(t, Config{}, menus, &fakeNotificationService{})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/menu", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	want := `{"status":200,"error":null,"data":[{"date":"2026-07-25","menus":["a","b","c"],"isValid":true}]}`
	assertJSONEqual(t, response.Body.Bytes(), []byte(want))
}

func TestTodayUsesConfiguredTimezone(t *testing.T) {
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	var received domain.LocalDate
	menus := &fakeMenuService{
		todayFn: func(_ context.Context, date domain.LocalDate) (domain.Menu, error) {
			received = date
			return domain.NewMenu(date, nil), nil
		},
	}
	handler := newTestHandler(t, Config{
		Location: seoul,
		Now: func() time.Time {
			return time.Date(2026, time.July, 25, 16, 30, 0, 0, time.UTC)
		},
	}, menus, &fakeNotificationService{})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/menu/today", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if received.String() != "2026-07-26" {
		t.Fatalf("Today date = %s, want 2026-07-26", received.String())
	}
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{"status":200,"error":null,"data":{"date":"2026-07-26","menus":[],"isValid":false}}`))
}

func TestHealthChecksOnlyServiceReadiness(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		handler := newTestHandler(t, Config{}, &fakeMenuService{}, &fakeNotificationService{})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		assertJSONEqual(t, response.Body.Bytes(), []byte(`{"status":200,"error":null,"data":{"ready":true}}`))
	})

	t.Run("database not ready", func(t *testing.T) {
		handler := newTestHandler(t, Config{}, &fakeMenuService{
			readyFn: func(context.Context) error { return errors.New("secret database detail") },
		}, &fakeNotificationService{})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "secret database detail") {
			t.Fatalf("health response exposed internal error: %s", response.Body.String())
		}
	})
}

func TestAdminEndpointsRequireConfiguredToken(t *testing.T) {
	t.Run("disabled when token is not configured", func(t *testing.T) {
		handler := newTestHandler(t, Config{}, &fakeMenuService{}, &fakeNotificationService{})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/crawl", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
		assertErrorCode(t, response.Body.Bytes(), "ADMIN_DISABLED")
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		handler := newTestHandler(t, Config{AdminToken: "correct-token"}, &fakeMenuService{}, &fakeNotificationService{})
		request := httptest.NewRequest(http.MethodPost, "/api/crawl", nil)
		request.Header.Set("Authorization", "Bearer wrong-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
		}
		if response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
		}
	})

	tests := []struct {
		name   string
		header string
		value  string
	}{
		{name: "bearer", header: "Authorization", value: "Bearer correct-token"},
		{name: "admin header", header: "X-Admin-Token", value: "correct-token"},
	}
	for _, test := range tests {
		t.Run("accepts "+test.name, func(t *testing.T) {
			calls := 0
			handler := newTestHandler(t, Config{AdminToken: "correct-token"}, &fakeMenuService{
				crawlFn: func(context.Context) error {
					calls++
					return nil
				},
			}, &fakeNotificationService{})
			request := httptest.NewRequest(http.MethodPost, "/api/crawl", nil)
			request.Header.Set(test.header, test.value)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || calls != 1 {
				t.Fatalf("status = %d, calls = %d: %s", response.Code, calls, response.Body.String())
			}
			assertJSONEqual(t, response.Body.Bytes(), []byte(`{"status":200,"error":null,"data":"ok"}`))
		})
	}
}

func TestLegacyAdminGETMethodsReturn405(t *testing.T) {
	for _, path := range []string{"/api/crawl", "/api/slack/notify"} {
		t.Run(path, func(t *testing.T) {
			handler := newTestHandler(t, Config{AdminToken: "token"}, &fakeMenuService{}, &fakeNotificationService{})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusMethodNotAllowed, response.Body.String())
			}
			if response.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow = %q, want POST", response.Header().Get("Allow"))
			}
			assertErrorCode(t, response.Body.Bytes(), "HttpRequestMethodNotSupportedException")
		})
	}
}

func TestManualMenuEndpointValidatesAndSavesOneMenu(t *testing.T) {
	date, err := domain.NewLocalDate(2026, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	var receivedDate domain.LocalDate
	var receivedMenus []string
	serviceCalls := 0
	menus := &fakeMenuService{
		saveManualFn: func(_ context.Context, gotDate domain.LocalDate, gotMenus []string) (domain.Menu, error) {
			serviceCalls++
			receivedDate = gotDate
			receivedMenus = append([]string(nil), gotMenus...)
			return domain.NewMenu(gotDate, gotMenus), nil
		},
	}
	handler := newTestHandler(t, Config{AdminToken: "secret"}, menus, &fakeNotificationService{})
	request := httptest.NewRequest(http.MethodPost, "/api/menu/manual", strings.NewReader(`{"date":"2026-10-01","menus":["one","side","kimchi"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Admin-Token", "secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if serviceCalls != 1 || receivedDate != date {
		t.Fatalf("SaveManual calls/date = %d/%s, want 1/%s", serviceCalls, receivedDate.String(), date.String())
	}
	if !reflect.DeepEqual(receivedMenus, []string{"one", "side", "kimchi"}) {
		t.Fatalf("menus = %#v, want input order preserved", receivedMenus)
	}
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{"status":200,"error":null,"data":{"date":"2026-10-01","menus":["one","side","kimchi"],"isValid":true}}`))
}

func TestManualMenuEndpointRejectsInvalidRequestsBeforeSaving(t *testing.T) {
	serviceCalls := 0
	menus := &fakeMenuService{
		saveManualFn: func(context.Context, domain.LocalDate, []string) (domain.Menu, error) {
			serviceCalls++
			return domain.Menu{}, nil
		},
	}
	handler := newTestHandler(t, Config{AdminToken: "secret"}, menus, &fakeNotificationService{})
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "malformed JSON", body: `{"date":"2026-10-01",`, code: "INVALID_REQUEST"},
		{name: "trailing JSON", body: `{"date":"2026-10-01","menus":["one","two","three"]}{}`, code: "INVALID_REQUEST"},
		{name: "invalid leap day", body: `{"date":"2026-02-29","menus":["one","two","three"]}`, code: "INVALID_DATE"},
		{name: "invalid date format", body: `{"date":"2026/10/01","menus":["one","two","three"]}`, code: "INVALID_DATE"},
		{name: "fewer than three menus", body: `{"date":"2026-10-01","menus":["one","two"]}`, code: "INVALID_MENUS"},
		{name: "empty menu item", body: `{"date":"2026-10-01","menus":["one"," ","three"]}`, code: "INVALID_MENUS"},
		{name: "unknown field", body: `{"date":"2026-10-01","menus":["one","two","three"],"valid":true}`, code: "INVALID_REQUEST"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/menu/manual", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer secret")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			assertErrorCode(t, response.Body.Bytes(), test.code)
		})
	}
	if serviceCalls != 0 {
		t.Fatalf("SaveManual calls = %d, want 0 for invalid requests", serviceCalls)
	}
}

func TestManualMenuEndpointRequiresAdminTokenAndPOST(t *testing.T) {
	body := `{"date":"2026-10-01","menus":["one","two","three"]}`

	t.Run("disabled", func(t *testing.T) {
		calls := 0
		menus := &fakeMenuService{saveManualFn: func(context.Context, domain.LocalDate, []string) (domain.Menu, error) {
			calls++
			return domain.Menu{}, nil
		}}
		handler := newTestHandler(t, Config{}, menus, &fakeNotificationService{})
		request := httptest.NewRequest(http.MethodPost, "/api/menu/manual", strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		assertErrorCode(t, response.Body.Bytes(), "ADMIN_DISABLED")
		if calls != 0 {
			t.Fatalf("SaveManual calls = %d, want 0", calls)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		calls := 0
		menus := &fakeMenuService{saveManualFn: func(context.Context, domain.LocalDate, []string) (domain.Menu, error) {
			calls++
			return domain.Menu{}, nil
		}}
		handler := newTestHandler(t, Config{AdminToken: "secret"}, menus, &fakeNotificationService{})
		request := httptest.NewRequest(http.MethodPost, "/api/menu/manual", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer wrong")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		assertErrorCode(t, response.Body.Bytes(), "UNAUTHORIZED")
		if calls != 0 {
			t.Fatalf("SaveManual calls = %d, want 0", calls)
		}
	})

	t.Run("GET is not allowed", func(t *testing.T) {
		handler := newTestHandler(t, Config{AdminToken: "secret"}, &fakeMenuService{}, &fakeNotificationService{})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/menu/manual", nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		if got := response.Header().Get("Allow"); got != http.MethodPost {
			t.Fatalf("Allow = %q, want POST", got)
		}
		assertErrorCode(t, response.Body.Bytes(), "HttpRequestMethodNotSupportedException")
	})
}

func TestNotifyEndpointUsesNotificationService(t *testing.T) {
	calls := 0
	handler := newTestHandler(t, Config{AdminToken: "token"}, &fakeMenuService{}, &fakeNotificationService{
		notifyFn: func(context.Context) error {
			calls++
			return nil
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/slack/notify", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d, calls = %d: %s", response.Code, calls, response.Body.String())
	}
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{"status":200,"error":null,"data":true}`))
}

func TestUploadStreamsToTemporaryFileAndCleansItUp(t *testing.T) {
	tempDir := t.TempDir()
	var servicePath string
	menus := &fakeMenuService{
		parseAndSaveFn: func(_ context.Context, path string) ([]domain.Menu, error) {
			servicePath = path
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read temporary upload: %v", err)
			}
			if string(contents) != "image contents" {
				t.Fatalf("temporary contents = %q", contents)
			}
			date, _ := domain.NewLocalDate(2026, 7, 25)
			return []domain.Menu{domain.NewMenu(date, []string{"a", "b", "c"})}, nil
		},
	}
	handler := newTestHandler(t, Config{
		AdminToken: "token",
		TempDir:    tempDir,
	}, menus, &fakeNotificationService{})
	request := multipartRequest(t, "/api/upload", "file", "menu.png", []byte("image contents"))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if servicePath == "" {
		t.Fatal("ParseAndSave was not called")
	}
	if _, err := os.Stat(servicePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary upload still exists after response: %v", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary directory contains %d entries", len(entries))
	}
}

func TestUploadEnforcesFileLimitAndRequiresFileField(t *testing.T) {
	parseCalls := 0
	menus := &fakeMenuService{
		parseAndSaveFn: func(context.Context, string) ([]domain.Menu, error) {
			parseCalls++
			return nil, nil
		},
	}
	handler := newTestHandler(t, Config{
		AdminToken:    "token",
		TempDir:       t.TempDir(),
		MaxUploadSize: 8,
	}, menus, &fakeNotificationService{})

	t.Run("larger than configured maximum", func(t *testing.T) {
		request := multipartRequest(t, "/api/upload", "file", "menu.png", []byte("123456789"))
		request.Header.Set("X-Admin-Token", "token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		assertErrorCode(t, response.Body.Bytes(), "UPLOAD_TOO_LARGE")
	})

	t.Run("wrong multipart field", func(t *testing.T) {
		request := multipartRequest(t, "/api/upload", "other", "menu.png", []byte("1234"))
		request.Header.Set("X-Admin-Token", "token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		assertErrorCode(t, response.Body.Bytes(), "MISSING_FILE")
	})

	t.Run("not multipart", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader("not multipart"))
		request.Header.Set("X-Admin-Token", "token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("exact maximum is accepted", func(t *testing.T) {
		request := multipartRequest(t, "/api/upload", "file", "menu.png", []byte("12345678"))
		request.Header.Set("X-Admin-Token", "token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
	})

	if parseCalls != 1 {
		t.Fatalf("ParseAndSave calls = %d, want 1", parseCalls)
	}
}

func TestServiceErrorsControlActualHTTPStatus(t *testing.T) {
	handler := newTestHandler(t, Config{}, &fakeMenuService{
		findAllFn: func(context.Context) ([]domain.Menu, error) {
			return nil, statusError{status: http.StatusConflict, code: "TEST_CONFLICT", message: "conflict"}
		},
	}, &fakeNotificationService{})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/menu", nil))

	if response.Code != http.StatusConflict {
		t.Fatalf("actual status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
	assertErrorCode(t, response.Body.Bytes(), "TEST_CONFLICT")
}

func TestAPIRoutesTakePriorityOverSPAFallback(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("SPA fallback"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, Config{StaticDir: staticDir}, &fakeMenuService{}, &fakeNotificationService{})

	spaResponse := httptest.NewRecorder()
	handler.ServeHTTP(spaResponse, httptest.NewRequest(http.MethodGet, "/client/route", nil))
	if spaResponse.Code != http.StatusOK || !strings.Contains(spaResponse.Body.String(), "SPA fallback") {
		t.Fatalf("SPA response status = %d, body = %q", spaResponse.Code, spaResponse.Body.String())
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/unknown", nil))
	if apiResponse.Code != http.StatusNotFound {
		t.Fatalf("API status = %d: %s", apiResponse.Code, apiResponse.Body.String())
	}
	if strings.Contains(apiResponse.Body.String(), "SPA fallback") {
		t.Fatalf("unknown API route reached SPA: %s", apiResponse.Body.String())
	}
	assertErrorCode(t, apiResponse.Body.Bytes(), "NOT_FOUND")
}

func TestPanicsAreRecoveredWithoutDetails(t *testing.T) {
	handler := newTestHandler(t, Config{}, &fakeMenuService{
		findAllFn: func(context.Context) ([]domain.Menu, error) {
			panic("sensitive panic detail")
		},
	}, &fakeNotificationService{})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/menu", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sensitive panic detail") {
		t.Fatalf("panic detail was exposed: %s", response.Body.String())
	}
	assertErrorCode(t, response.Body.Bytes(), "INTERNAL_SERVER_ERROR")
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	handler := newTestHandler(t, Config{RequestTimeout: 20 * time.Millisecond}, &fakeMenuService{
		findAllFn: func(context.Context) ([]domain.Menu, error) {
			<-release
			return nil, nil
		},
	}, &fakeNotificationService{})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/menu", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	assertErrorCode(t, response.Body.Bytes(), "REQUEST_TIMEOUT")
}

func newTestHandler(t *testing.T, config Config, menus MenuService, notifications NotificationService) http.Handler {
	t.Helper()
	config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewHandler(config, menus, notifications)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return handler
}

func multipartRequest(t *testing.T, target, field, name string, contents []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func assertJSONEqual(t *testing.T, actual, expected []byte) {
	t.Helper()
	var actualValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("decode actual JSON: %v: %s", err, actual)
	}
	var expectedValue any
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		t.Fatalf("decode expected JSON: %v: %s", err, expected)
	}
	actualJSON, _ := json.Marshal(actualValue)
	expectedJSON, _ := json.Marshal(expectedValue)
	if !bytes.Equal(actualJSON, expectedJSON) {
		t.Fatalf("JSON mismatch\nactual:   %s\nexpected: %s", actualJSON, expectedJSON)
	}
}

func assertErrorCode(t *testing.T, body []byte, expected string) {
	t.Helper()
	var decoded struct {
		Error *errorResponse `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode error response: %v: %s", err, body)
	}
	if decoded.Error == nil || decoded.Error.ErrorCode != expected {
		t.Fatalf("error = %#v, want code %q", decoded.Error, expected)
	}
}

type statusError struct {
	status  int
	code    string
	message string
}

func (err statusError) Error() string     { return err.message }
func (err statusError) HTTPStatus() int   { return err.status }
func (err statusError) ErrorCode() string { return err.code }
