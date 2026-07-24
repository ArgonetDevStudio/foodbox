package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerServesStaticAsset(t *testing.T) {
	handler := NewFS(testAssets())
	request := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "console.log('foodbox')" {
		t.Fatalf("body = %q", body)
	}
}

func TestHandlerFallsBackToIndexForSPARoute(t *testing.T) {
	handler := NewFS(testAssets())
	request := httptest.NewRequest(http.MethodGet, "/menus/2026-07-25", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "<html>foodbox</html>" {
		t.Fatalf("body = %q", body)
	}
}

func TestHandlerDoesNotInterceptAPIOrHealthRoutes(t *testing.T) {
	handler := NewFS(testAssets())
	for _, requestPath := range []string{
		"/api",
		"/api/menu/today",
		"/healthz",
		"/healthz/ready",
		"/menus/../api/menu/today",
	} {
		t.Run(requestPath, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
			}
			if strings.Contains(response.Body.String(), "foodbox") {
				t.Fatal("reserved route received SPA fallback")
			}
		})
	}
}

func TestHandlerAllowsSimilarNonReservedPaths(t *testing.T) {
	handler := NewFS(testAssets())
	for _, requestPath := range []string{"/apiary", "/healthzone"} {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", requestPath, response.Code, http.StatusOK)
		}
	}
}

func TestHandlerWorksWhenStaticDirectoryIsMissing(t *testing.T) {
	handler := New(t.TempDir() + "/missing")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestHandlerWorksWithNilFileSystem(t *testing.T) {
	handler := NewFS(nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestHandlerRejectsMutationMethods(t *testing.T) {
	handler := NewFS(testAssets())
	request := httptest.NewRequest(http.MethodPost, "/menus", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestHandlerSupportsHead(t *testing.T) {
	handler := NewFS(testAssets())
	request := httptest.NewRequest(http.MethodHead, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("HEAD response body length = %d, want 0", response.Body.Len())
	}
}

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>foodbox</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('foodbox')")},
	}
}
