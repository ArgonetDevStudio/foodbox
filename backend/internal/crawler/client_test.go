package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDownloadFindsCurrentMonthAndResolvesRelativeURLs(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0x00, 0x01}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/board/list":
			_, _ = fmt.Fprint(w, `<div class="bbs-list">
				<a class="aline" href="/board/old">2026년 06월 식단표</a>
				<a class="aline" href="detail/current">2026년 07월 식단표 안내</a>
			</div>`)
		case "/board/detail/current":
			_, _ = fmt.Fprint(w, `<div id="bo_v_con"><img src="../../images/menu.jpg"></div>`)
		case "/images/menu.jpg":
			_, _ = w.Write(jpeg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/board/list", withClock(func() time.Time {
		return time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
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
	if string(got) != string(jpeg) {
		t.Fatalf("downloaded %v", got)
	}
}

func TestDownloadFallsBackToAnchor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprint(w, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = fmt.Fprint(w, `<div id="bo_v_con"><a href="/download/menu.png">download</a></div>`)
		case "/download/menu.png":
			_, _ = w.Write([]byte("image"))
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL+"/list", withClock(func() time.Time {
		return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}))
	path, err := client.Download(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("caller could not clean up temporary file: %v", err)
	}
}

func TestDownloadRejectsOversizedImageAndRemovesTemporaryFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprint(w, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = fmt.Fprint(w, `<div id="bo_v_con"><img src="/large"></div>`)
		case "/large":
			_, _ = fmt.Fprint(w, "12345")
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL+"/list",
		withClock(func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }),
		WithLimits(1024, 4),
	)
	path, err := client.Download(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("path = %q, error = %v", path, err)
	}
	if path != "" {
		t.Fatalf("failed download exposed temporary path %q", path)
	}
}

func TestDownloadRejectsOversizedHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", 5))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, WithLimits(4, 1024))
	if _, err := client.Download(context.Background()); err == nil {
		t.Fatal("expected HTML size error")
	}
}

func TestDownloadRequiresCurrentMonthLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<div class="bbs-list"><a class="aline" href="/detail">2026년 06월 식단표</a></div>`)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, withClock(func() time.Time {
		return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}))
	if _, err := client.Download(context.Background()); err == nil {
		t.Fatal("expected missing menu error")
	}
}
