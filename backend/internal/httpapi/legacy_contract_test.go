package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
	menuservice "github.com/LooLookProject/foodbox/backend/internal/service"
	"github.com/LooLookProject/foodbox/backend/internal/store"
)

// These tests pin the externally observable menu API contract during the
// Spring-to-Go migration. API dates were ISO strings in Spring even though
// persisted LocalDate values are arrays.
func TestLegacyContractMenuCollection(t *testing.T) {
	newest := mustContractDate(t, 2026, 7, 27)
	middle := mustContractDate(t, 2026, 7, 25)
	oldest := mustContractDate(t, 2025, 12, 31)

	repository, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	// Save out of order. The legacy endpoint exposed newest first, independently
	// of the oldest-first database file order.
	if err := repository.SaveAll(context.Background(), []domain.Menu{
		domain.NewMenu(middle, []string{"two items", "are invalid"}),
		domain.NewMenu(oldest, []string{"old one", "old two", "old three"}),
		domain.NewMenu(newest, []string{"new one", "new two", "new three", "new four"}),
	}); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	menus := menuservice.NewMenuService(repository, nil, nil, nil, nil)
	handler := newTestHandler(t, Config{}, menus, &fakeNotificationService{})
	response := performContractRequest(handler, http.MethodGet, "/api/menu", "")

	assertContractHTTP(t, response, http.StatusOK)
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{
		"status": 200,
		"error": null,
		"data": [
			{"date":"2026-07-27","menus":["new one","new two","new three","new four"],"isValid":true},
			{"date":"2026-07-25","menus":["two items","are invalid"],"isValid":false},
			{"date":"2025-12-31","menus":["old one","old two","old three"],"isValid":true}
		]
	}`))
	assertMenuResponseKeys(t, response.Body.Bytes(), 3)
}

func TestLegacyContractMenuCollectionIsNeverNull(t *testing.T) {
	handler := newTestHandler(t, Config{}, &fakeMenuService{
		findAllFn: func(context.Context) ([]domain.Menu, error) {
			return []domain.Menu{}, nil
		},
	}, &fakeNotificationService{})
	response := performContractRequest(handler, http.MethodGet, "/api/menu", "")

	assertContractHTTP(t, response, http.StatusOK)
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{"status":200,"error":null,"data":[]}`))
}

func TestLegacyContractTodayOnWeekday(t *testing.T) {
	date := mustContractDate(t, 2026, 7, 27) // Monday
	var received domain.LocalDate
	handler := newTestHandler(t, contractClockConfig(date), &fakeMenuService{
		todayFn: func(_ context.Context, actual domain.LocalDate) (domain.Menu, error) {
			received = actual
			return domain.NewMenu(actual, []string{"rice", "soup", "kimchi"}), nil
		},
	}, &fakeNotificationService{})
	response := performContractRequest(handler, http.MethodGet, "/api/menu/today", "")

	assertContractHTTP(t, response, http.StatusOK)
	if received != date {
		t.Fatalf("Today received date = %s, want %s", received, date)
	}
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{
		"status":200,
		"error":null,
		"data":{"date":"2026-07-27","menus":["rice","soup","kimchi"],"isValid":true}
	}`))
	assertMenuResponseKeys(t, response.Body.Bytes(), 1)
}

func TestLegacyContractTodayOnWeekend(t *testing.T) {
	date := mustContractDate(t, 2026, 7, 26) // Sunday
	repository, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	menus := menuservice.NewMenuService(repository, nil, nil, nil, nil)
	handler := newTestHandler(t, contractClockConfig(date), menus, &fakeNotificationService{})
	response := performContractRequest(handler, http.MethodGet, "/api/menu/today", "")

	assertContractHTTP(t, response, http.StatusOK)
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{
		"status":200,
		"error":null,
		"data":{"date":"2026-07-26","menus":["주말에는 도시락이 없습니다."],"isValid":false}
	}`))
}

func TestLegacyContractTodayWhenMenuIsMissing(t *testing.T) {
	date := mustContractDate(t, 2026, 7, 27)
	handler := newTestHandler(t, contractClockConfig(date), &fakeMenuService{
		todayFn: func(context.Context, domain.LocalDate) (domain.Menu, error) {
			return domain.Menu{}, menuservice.ErrMenuNotUploaded
		},
	}, &fakeNotificationService{})
	response := performContractRequest(handler, http.MethodGet, "/api/menu/today", "")

	// Spring put 404 in the envelope while its catch-all advice returned HTTP
	// 200. Go intentionally makes the real HTTP status agree with the envelope.
	assertContractHTTP(t, response, http.StatusNotFound)
	assertJSONEqual(t, response.Body.Bytes(), []byte(`{
		"status":404,
		"error":{"errorCode":"MENU_NOT_UPLOADED","message":"Today Menu is not uploaded yet"},
		"data":null
	}`))
}

func TestLegacyContractPublicMenuMethodsAndContentType(t *testing.T) {
	handler := newTestHandler(t, Config{}, &fakeMenuService{}, &fakeNotificationService{})

	for _, test := range []struct {
		path   string
		method string
	}{
		{path: "/api/menu", method: http.MethodPost},
		{path: "/api/menu/today", method: http.MethodPut},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := performContractRequest(handler, test.method, test.path, "")
			assertContractHTTP(t, response, http.StatusMethodNotAllowed)
			if got := response.Header().Get("Allow"); got != http.MethodGet {
				t.Fatalf("Allow = %q, want %q", got, http.MethodGet)
			}
			wantBody := `{"status":405,"error":{"errorCode":"HttpRequestMethodNotSupportedException","message":"Request method '` +
				test.method + `' is not supported"},"data":null}` + "\n"
			if got := response.Body.String(); got != wantBody {
				t.Fatalf("body = %q, want exact Spring-compatible envelope %q", got, wantBody)
			}
		})
	}
}

func TestIntentionalAdminContractChangesAreProtected(t *testing.T) {
	t.Run("legacy state-changing GET routes are no longer accepted", func(t *testing.T) {
		for _, path := range []string{"/api/crawl", "/api/slack/notify"} {
			t.Run(path, func(t *testing.T) {
				handler := newTestHandler(t, Config{AdminToken: "secret"}, &fakeMenuService{}, &fakeNotificationService{})
				response := performContractRequest(handler, http.MethodGet, path, "")
				assertContractHTTP(t, response, http.StatusMethodNotAllowed)
				if got := response.Header().Get("Allow"); got != http.MethodPost {
					t.Fatalf("Allow = %q, want POST", got)
				}
				assertErrorEnvelope(t, response.Body.Bytes(), http.StatusMethodNotAllowed, "HttpRequestMethodNotSupportedException")
			})
		}
	})

	t.Run("all management POST routes reject unauthenticated callers", func(t *testing.T) {
		calls := 0
		menus := &fakeMenuService{
			crawlFn: func(context.Context) error { calls++; return nil },
			parseAndSaveFn: func(context.Context, string) ([]domain.Menu, error) {
				calls++
				return []domain.Menu{}, nil
			},
		}
		notifications := &fakeNotificationService{
			notifyFn: func(context.Context) error { calls++; return nil },
		}
		handler := newTestHandler(t, Config{AdminToken: "secret", TempDir: t.TempDir()}, menus, notifications)

		requests := []*http.Request{
			httptest.NewRequest(http.MethodPost, "/api/crawl", nil),
			httptest.NewRequest(http.MethodPost, "/api/slack/notify", nil),
			multipartRequest(t, "/api/upload", "file", "menu.png", []byte("image")),
		}
		for _, request := range requests {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertContractHTTP(t, response, http.StatusUnauthorized)
			if got := response.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("%s WWW-Authenticate = %q, want Bearer", request.URL.Path, got)
			}
			assertErrorEnvelope(t, response.Body.Bytes(), http.StatusUnauthorized, "UNAUTHORIZED")
		}
		if calls != 0 {
			t.Fatalf("management service calls = %d, want 0 before authentication", calls)
		}
	})

	t.Run("management endpoints are disabled without a configured token", func(t *testing.T) {
		handler := newTestHandler(t, Config{}, &fakeMenuService{}, &fakeNotificationService{})
		response := performContractRequest(handler, http.MethodPost, "/api/crawl", "")
		assertContractHTTP(t, response, http.StatusServiceUnavailable)
		assertErrorEnvelope(t, response.Body.Bytes(), http.StatusServiceUnavailable, "ADMIN_DISABLED")
	})

	t.Run("authenticated replacement routes retain their successful values", func(t *testing.T) {
		crawlCalls := 0
		notifyCalls := 0
		handler := newTestHandler(t, Config{AdminToken: "secret"}, &fakeMenuService{
			crawlFn: func(context.Context) error { crawlCalls++; return nil },
		}, &fakeNotificationService{
			notifyFn: func(context.Context) error { notifyCalls++; return nil },
		})

		tests := []struct {
			path string
			want string
		}{
			{path: "/api/crawl", want: `{"status":200,"error":null,"data":"ok"}`},
			{path: "/api/slack/notify", want: `{"status":200,"error":null,"data":true}`},
		}
		for _, test := range tests {
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			request.Header.Set("Authorization", "Bearer secret")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertContractHTTP(t, response, http.StatusOK)
			assertJSONEqual(t, response.Body.Bytes(), []byte(test.want))
		}
		if crawlCalls != 1 || notifyCalls != 1 {
			t.Fatalf("crawl calls = %d, notify calls = %d, want 1 each", crawlCalls, notifyCalls)
		}
	})
}

func mustContractDate(t *testing.T, year, month, day int) domain.LocalDate {
	t.Helper()
	date, err := domain.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("NewLocalDate: %v", err)
	}
	return date
}

func contractClockConfig(date domain.LocalDate) Config {
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	return Config{
		Location: seoul,
		Now: func() time.Time {
			return time.Date(date.Year, time.Month(date.Month), date.Day, 12, 0, 0, 0, seoul)
		},
	}
}

func performContractRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertContractHTTP(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("HTTP status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	const contentType = "application/json; charset=utf-8"
	if got := response.Header().Get("Content-Type"); got != contentType {
		t.Fatalf("Content-Type = %q, want %q", got, contentType)
	}
}

func assertErrorEnvelope(t *testing.T, body []byte, status int, code string) {
	t.Helper()
	var decoded struct {
		Status int            `json:"status"`
		Error  *errorResponse `json:"error"`
		Data   any            `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode error envelope: %v: %s", err, body)
	}
	if decoded.Status != status || decoded.Error == nil || decoded.Error.ErrorCode != code || decoded.Data != nil {
		t.Fatalf("error envelope = %#v, want status=%d code=%q data=nil", decoded, status, code)
	}
}

func assertMenuResponseKeys(t *testing.T, body []byte, expectedMenus int) {
	t.Helper()
	var decoded struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	var list []map[string]json.RawMessage
	if len(decoded.Data) > 0 && decoded.Data[0] == '[' {
		if err := json.Unmarshal(decoded.Data, &list); err != nil {
			t.Fatalf("decode menu list: %v", err)
		}
	} else {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(decoded.Data, &item); err != nil {
			t.Fatalf("decode menu item: %v", err)
		}
		list = []map[string]json.RawMessage{item}
	}
	if len(list) != expectedMenus {
		t.Fatalf("menu response count = %d, want %d", len(list), expectedMenus)
	}
	for index, item := range list {
		if len(item) != 3 || item["date"] == nil || item["menus"] == nil || item["isValid"] == nil {
			t.Fatalf("menu %d keys = %v, want exactly date, menus, isValid", index, item)
		}
		if item["valid"] != nil {
			t.Fatalf("menu %d leaked persistence field valid", index)
		}
	}
}
