package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsLegacyPayloadAndNormalizesURLAndChannel(t *testing.T) {
	var received Message
	var requestPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/hooks/", "/secret-token", WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	message := Message{Channel: "lunch", Username: "foodbox", Text: "menu", IconEmoji: ":rice:"}
	if err := client.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if requestPath != "/hooks/secret-token" {
		t.Fatalf("path = %q", requestPath)
	}
	if received.Channel != "#lunch" || received.Username != "foodbox" || received.Text != "menu" || received.IconEmoji != ":rice:" {
		t.Fatalf("payload = %+v", received)
	}
}

func TestSendReturnsNon2xxStatusAndResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, "invalid_payload")
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "secret", WithHTTPClient(server.Client()))
	err := client.Send(context.Background(), Message{Text: "menu"})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked token: %v", err)
	}
}

type leakingTransport struct{}

func (leakingTransport) Do(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("request to secret-token failed")
}

func TestSendDoesNotLeakTokenFromTransportError(t *testing.T) {
	client, _ := NewClient("https://example.com/hooks", "secret-token", WithHTTPClient(leakingTransport{}))
	err := client.Send(context.Background(), Message{})
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestSendLimitsResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "12345")
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "secret", WithHTTPClient(server.Client()), WithMaxResponseSize(4))
	if err := client.Send(context.Background(), Message{}); err == nil {
		t.Fatal("expected response size error")
	}
}

func TestNormalizeChannel(t *testing.T) {
	tests := map[string]string{
		"lunch":   "#lunch",
		"#lunch":  "#lunch",
		" lunch ": "#lunch",
		"":        "",
	}
	for input, want := range tests {
		if got := NormalizeChannel(input); got != want {
			t.Errorf("NormalizeChannel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewClientRejectsHTTP(t *testing.T) {
	if _, err := NewClient("http://example.com/hooks", "secret"); err == nil {
		t.Fatal("expected HTTP endpoint rejection")
	}
}

func TestSendDoesNotFollowRedirectContainingTokenPath(t *testing.T) {
	redirectReached := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectReached <- struct{}{}
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hooks/secret-token" {
			t.Errorf("source path = %q", r.URL.Path)
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	httpClient := newHTTPClient(source.Client().Transport)
	client, err := NewClient(source.URL+"/hooks", "secret-token", WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	err = client.Send(context.Background(), Message{Text: "menu"})
	if err == nil || !strings.Contains(err.Error(), "307") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe error = %v", err)
	}
	select {
	case <-redirectReached:
		t.Fatal("redirect target received token-bearing request")
	default:
	}
}
