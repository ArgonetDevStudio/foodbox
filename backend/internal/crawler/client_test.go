package crawler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func newCrawlerTLSServer(t *testing.T, handler http.Handler) (string, *http.Client) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	host := net.JoinHostPort("example.com", port)
	transport := server.Client().Transport.(*http.Transport).Clone()
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != host {
			return nil, fmt.Errorf("unexpected test address %q", address)
		}
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	return "https://" + host, &http.Client{Transport: transport}
}

func TestDownloadFindsCurrentMonthAndResolvesRelativeURLs(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0x00, 0x01}
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	client, err := NewClient(serverURL+"/board/list", WithHTTPClient(httpClient), withClock(func() time.Time {
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
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprint(w, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = fmt.Fprint(w, `<div id="bo_v_con"><a href="/download/menu.png">download</a></div>`)
		case "/download/menu.png":
			_, _ = w.Write([]byte("image"))
		}
	}))

	client, _ := NewClient(serverURL+"/list", WithHTTPClient(httpClient), withClock(func() time.Time {
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
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprint(w, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = fmt.Fprint(w, `<div id="bo_v_con"><img src="/large"></div>`)
		case "/large":
			_, _ = fmt.Fprint(w, "12345")
		}
	}))

	client, _ := NewClient(serverURL+"/list",
		WithHTTPClient(httpClient),
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
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", 5))
	}))

	client, _ := NewClient(serverURL, WithHTTPClient(httpClient), WithLimits(4, 1024))
	if _, err := client.Download(context.Background()); err == nil {
		t.Fatal("expected HTML size error")
	}
}

func TestDownloadRequiresCurrentMonthLink(t *testing.T) {
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<div class="bbs-list"><a class="aline" href="/detail">2026년 06월 식단표</a></div>`)
	}))

	client, _ := NewClient(serverURL, WithHTTPClient(httpClient), withClock(func() time.Time {
		return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}))
	if _, err := client.Download(context.Background()); err == nil {
		t.Fatal("expected missing menu error")
	}
}

func TestNewClientRequiresSecurePublicCredentialFreeOrigin(t *testing.T) {
	tests := []string{
		"https://:443/list",
		"http://vendor.example.test/list",
		"ftp://vendor.example.test/list",
		"https://user:secret@vendor.example.test/list",
		"https://localhost/list",
		"https://service.localhost/list",
		"https://0.0.0.1/list",
		"https://127.0.0.1/list",
		"https://169.254.10.20/list",
		"https://10.0.0.1/list",
		"https://100.64.0.1/list",
		"https://198.18.0.1/list",
		"https://[fe80::1]/list",
		"https://[64:ff9b::a9fe:a9fe]/list",
		"https://[2002:a9fe:a9fe::1]/list",
		"https://[fec0::1]/list",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			if _, err := NewClient(target); err == nil {
				t.Fatalf("NewClient(%q) succeeded", target)
			} else if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked URL credentials: %v", err)
			}
		})
	}
}

func TestResolvePublicAddressesRejectsPrivateAndMixedDNSAnswers(t *testing.T) {
	tests := []struct {
		name      string
		addresses []net.IPAddr
		wantError bool
	}{
		{
			name:      "public only",
			addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}},
		},
		{
			name:      "loopback",
			addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
			wantError: true,
		},
		{
			name:      "IPv4 this network",
			addresses: []net.IPAddr{{IP: net.ParseIP("0.0.0.1")}},
			wantError: true,
		},
		{
			name: "mixed public and private",
			addresses: []net.IPAddr{
				{IP: net.ParseIP("93.184.216.34")},
				{IP: net.ParseIP("169.254.169.254")},
			},
			wantError: true,
		},
		{
			name:      "IPv6 unique local",
			addresses: []net.IPAddr{{IP: net.ParseIP("fd00::1")}},
			wantError: true,
		},
		{
			name:      "carrier-grade NAT",
			addresses: []net.IPAddr{{IP: net.ParseIP("100.64.0.1")}},
			wantError: true,
		},
		{
			name:      "benchmark network",
			addresses: []net.IPAddr{{IP: net.ParseIP("198.18.0.1")}},
			wantError: true,
		},
		{
			name:      "NAT64 metadata mapping",
			addresses: []net.IPAddr{{IP: net.ParseIP("64:ff9b::a9fe:a9fe")}},
			wantError: true,
		},
		{
			name:      "6to4 metadata mapping",
			addresses: []net.IPAddr{{IP: net.ParseIP("2002:a9fe:a9fe::1")}},
			wantError: true,
		},
		{
			name:      "deprecated IPv6 site local",
			addresses: []net.IPAddr{{IP: net.ParseIP("fec0::1")}},
			wantError: true,
		},
		{
			name:      "empty address",
			addresses: []net.IPAddr{{}},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := staticResolver{addresses: test.addresses}
			addresses, err := resolvePublicAddresses(context.Background(), resolver, "vendor.example.test")
			if test.wantError {
				if err == nil {
					t.Fatalf("addresses = %v, want rejection", addresses)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(addresses) != len(test.addresses) {
				t.Fatalf("addresses = %v, want %v", addresses, test.addresses)
			}
		})
	}
}

func TestDefaultClientDisablesProxyAndUsesPublicDestinationDialer(t *testing.T) {
	client, err := NewClient("https://vendor.example.test/list")
	if err != nil {
		t.Fatal(err)
	}
	httpClient, ok := client.httpClient.(*http.Client)
	if !ok {
		t.Fatalf("HTTP client type = %T", client.httpClient)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("default crawler transport must not delegate target resolution to a proxy")
	}
	if transport.DialContext == nil {
		t.Fatal("default crawler transport must validate resolved destination addresses")
	}
}

func TestResolveURLAllowsOnlySameHTTPSOrigin(t *testing.T) {
	client, err := NewClient("https://vendor.example.test/board/list")
	if err != nil {
		t.Fatal(err)
	}

	allowed := []string{
		"detail/current",
		"/images/menu.jpg",
		"https://vendor.example.test/download/menu.jpg",
		"//vendor.example.test/assets/menu.jpg",
	}
	for _, reference := range allowed {
		t.Run("allow_"+reference, func(t *testing.T) {
			if _, err := client.resolveURL(client.listURL, reference); err != nil {
				t.Fatalf("resolveURL(%q): %v", reference, err)
			}
		})
	}

	rejected := []string{
		"https://other.example.test/menu.jpg?token=sensitive-value",
		"//other.example.test/menu.jpg?token=sensitive-value",
		"http://vendor.example.test/menu.jpg?token=sensitive-value",
		"https://vendor.example.test:444/menu.jpg?token=sensitive-value",
		"https://user:sensitive-value@vendor.example.test/menu.jpg",
		"file:///etc/passwd",
	}
	for _, reference := range rejected {
		t.Run("reject_"+reference, func(t *testing.T) {
			_, err := client.resolveURL(client.listURL, reference)
			if err == nil {
				t.Fatalf("resolveURL(%q) succeeded", reference)
			}
			if strings.Contains(err.Error(), "sensitive-value") {
				t.Fatalf("error leaked page-controlled URL: %v", err)
			}
		})
	}
}

func TestDownloadAllowsSameOriginRedirect(t *testing.T) {
	image := []byte("image")
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/list":
			http.Redirect(writer, request, "/actual-list", http.StatusFound)
		case "/actual-list":
			_, _ = fmt.Fprint(writer, `<div class="bbs-list"><a class="aline" href="/detail">2026년 07월 식단표</a></div>`)
		case "/detail":
			_, _ = fmt.Fprint(writer, `<div id="bo_v_con"><img src="/image"></div>`)
		case "/image":
			_, _ = writer.Write(image)
		default:
			http.NotFound(writer, request)
		}
	}))
	client, err := NewClient(serverURL+"/list", WithHTTPClient(httpClient), withClock(func() time.Time {
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
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(image) {
		t.Fatalf("download = %q", got)
	}
}

func TestDownloadRejectsCrossOriginRedirectBeforeRequest(t *testing.T) {
	serverURL, httpClient := newCrawlerTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", "https://internal.example.test/private?token=sensitive-value")
		writer.WriteHeader(http.StatusFound)
	}))
	client, err := NewClient(serverURL+"/list", WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Download(context.Background())
	if err == nil {
		t.Fatal("expected cross-origin redirect rejection")
	}
	if strings.Contains(err.Error(), "internal.example.test") || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("error leaked redirect target: %v", err)
	}
}

type staticResolver struct {
	addresses []net.IPAddr
	err       error
}

func (resolver staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.addresses, resolver.err
}
