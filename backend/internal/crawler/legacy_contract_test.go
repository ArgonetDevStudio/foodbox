package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLegacyContractDownloadUsesEisoSelectorsCurrentMonthAndRelativeURLs(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0x10, 0x20}
	var mutex sync.Mutex
	var requestURIs []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", request.Method)
		}
		if got := request.Header.Get("User-Agent"); got != "foodbox/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		mutex.Lock()
		requestURIs = append(requestURIs, request.URL.RequestURI())
		mutex.Unlock()

		switch request.URL.RequestURI() {
		case "/bbs/board.php?bo_table=basic4":
			_, _ = io.WriteString(writer, `<a class="aline" href="/outside">2026년 07월 식단표</a>
				<div class="bbs-list">
				  <a class="other" href="/wrong-class">2026년 07월 식단표</a>
				  <a class="aline" href="old">2026년 06월 식단표</a>
				  <a class="aline" href="detail/current?from=list"> 2026년   07월 월간 식단표 안내 </a>
				  <a class="aline" href="/second-match">2026년 07월 식단표</a>
				</div>`)
		case "/bbs/detail/current?from=list":
			_, _ = io.WriteString(writer, `<div id="unrelated"><img src="/wrong.jpg"></div>
				<div id="bo_v_con">
				  <img src="../../images/menu.jpg?download=1">
				  <img src="/second.jpg">
				</div>`)
		case "/images/menu.jpg?download=1":
			writer.Header().Set("Content-Type", "image/jpeg")
			_, _ = writer.Write(image)
		default:
			http.Error(writer, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/bbs/board.php?bo_table=basic4", withClock(func() time.Time {
		return time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	}))
	if err != nil {
		t.Fatal(err)
	}
	path, err := client.Download(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	downloaded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, image) {
		t.Fatalf("downloaded = %v", downloaded)
	}
	mutex.Lock()
	gotURIs := append([]string(nil), requestURIs...)
	mutex.Unlock()
	wantURIs := []string{
		"/bbs/board.php?bo_table=basic4",
		"/bbs/detail/current?from=list",
		"/images/menu.jpg?download=1",
	}
	if !reflect.DeepEqual(gotURIs, wantURIs) {
		t.Fatalf("request URIs = %#v, want %#v", gotURIs, wantURIs)
	}
}

func TestLegacyContractDownloadFallsBackToFirstAnchorOnlyInsideContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/list":
			_, _ = io.WriteString(writer, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = io.WriteString(writer, `<a href="/outside">outside</a><div id="bo_v_con"><a href="assets/menu.png">menu</a></div>`)
		case "/assets/menu.png":
			_, _ = io.WriteString(writer, "png-bytes")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/list", withClock(func() time.Time {
		return time.Date(2026, time.July, 31, 23, 59, 59, 0, koreaLocation)
	}))
	if err != nil {
		t.Fatal(err)
	}
	path, err := client.Download(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "png-bytes" {
		t.Fatalf("download = %q", got)
	}
}

func TestLegacyContractCrawlerDefaultHTTPClientHasBoundedTimeout(t *testing.T) {
	client, err := NewClient("https://eisodosirak.example.test/list")
	if err != nil {
		t.Fatal(err)
	}
	httpClient, ok := client.httpClient.(*http.Client)
	if !ok {
		t.Fatalf("default HTTP client type = %T", client.httpClient)
	}
	if defaultTimeout != 15*time.Second {
		t.Fatalf("contract timeout constant = %s", defaultTimeout)
	}
	if httpClient.Timeout != defaultTimeout {
		t.Fatalf("timeout = %s", httpClient.Timeout)
	}
}

func TestLegacyContractDownloadRejectsEveryNon2xxStage(t *testing.T) {
	tests := []struct {
		name       string
		failedPath string
		status     int
	}{
		{name: "list", failedPath: "/list", status: http.StatusServiceUnavailable},
		{name: "detail", failedPath: "/detail", status: http.StatusNotFound},
		{name: "image", failedPath: "/image", status: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == test.failedPath {
					http.Error(writer, "failure", test.status)
					return
				}
				switch request.URL.Path {
				case "/list":
					_, _ = io.WriteString(writer, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
				case "/detail":
					_, _ = io.WriteString(writer, `<div id="bo_v_con"><img src="/image"></div>`)
				case "/image":
					_, _ = io.WriteString(writer, "image")
				}
			}))
			defer server.Close()

			client, err := NewClient(server.URL+"/list", withClock(func() time.Time {
				return time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
			}))
			if err != nil {
				t.Fatal(err)
			}
			path, err := client.Download(context.Background())
			if err == nil || path != "" || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", test.status)) {
				t.Fatalf("path = %q, error = %v", path, err)
			}
		})
	}
}

func TestLegacyContractOversizedStreamRemovesTemporaryDownload(t *testing.T) {
	temporaryDirectory := t.TempDir()
	t.Setenv("TMPDIR", temporaryDirectory)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/list":
			_, _ = io.WriteString(writer, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = io.WriteString(writer, `<div id="bo_v_con"><img src="/stream"></div>`)
		case "/stream":
			writer.(http.Flusher).Flush()
			_, _ = io.WriteString(writer, "12345")
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/list",
		withClock(func() time.Time { return time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC) }),
		WithLimits(1024, 4),
	)
	if err != nil {
		t.Fatal(err)
	}
	path, err := client.Download(context.Background())
	if err == nil || path != "" || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("path = %q, error = %v", path, err)
	}
	remaining, err := filepath.Glob(filepath.Join(temporaryDirectory, "foodbox-menu-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("temporary files were not removed: %v", remaining)
	}
}
