package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLegacyContractSendUsesExactWebhookPathHeadersAndPayload(t *testing.T) {
	wantBody := []byte(`{"channel":"#foodbox","username":"점심봇","text":"\u003c2026-07-27 월요일\u003e 점심 메뉴\n• 김치찌개\n• 제육볶음","icon_emoji":":bento:"}`)
	requests := make(chan []byte, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if request.URL.RequestURI() != "/services/T000/B000/webhook-secret" {
			t.Errorf("request URI = %q", request.URL.RequestURI())
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		if request.ContentLength != int64(len(body)) {
			t.Errorf("Content-Length = %d, body length = %d", request.ContentLength, len(body))
		}
		requests <- body
		_, _ = io.WriteString(writer, "ok")
	}))
	defer server.Close()

	client, err := NewClient(
		server.URL+"/services/",
		"/T000/B000/webhook-secret",
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	message := Message{
		Channel:   " foodbox ",
		Username:  "점심봇",
		Text:      "<2026-07-27 월요일> 점심 메뉴\n• 김치찌개\n• 제육볶음",
		IconEmoji: ":bento:",
	}
	if err := client.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; !bytes.Equal(got, wantBody) {
		t.Fatalf("request body = %s\nwant = %s", got, wantBody)
	}
	if message.Channel != " foodbox " {
		t.Fatalf("Send mutated caller message: %+v", message)
	}
}

func TestLegacyContractSlackDefaultClientHasBoundedTimeoutAndRejectsRedirects(t *testing.T) {
	client := newHTTPClient(http.DefaultTransport)
	if defaultTimeout != 10*time.Second {
		t.Fatalf("contract timeout constant = %s", defaultTimeout)
	}
	if client.Timeout != defaultTimeout {
		t.Fatalf("timeout = %s", client.Timeout)
	}
	request, err := http.NewRequest(http.MethodPost, "https://hooks.slack.com/services/T/B/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	redirectErr := client.CheckRedirect(request, []*http.Request{request})
	if !errors.Is(redirectErr, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error = %v", redirectErr)
	}
}

func TestLegacyContractSlackRedirectNeverForwardsWebhookToken(t *testing.T) {
	const token = "T000/B000/never-forward-token"
	targetRequests := make(chan *http.Request, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		targetRequests <- request.Clone(context.Background())
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/services/"+token {
			t.Errorf("source path = %q", request.URL.Path)
		}
		writer.Header().Set("Location", target.URL+"/collect")
		writer.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer source.Close()

	client, err := NewClient(
		source.URL+"/services",
		token,
		WithHTTPClient(newHTTPClient(source.Client().Transport)),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Send(context.Background(), Message{Text: "menu"})
	if err == nil || !strings.Contains(err.Error(), "308") || strings.Contains(err.Error(), token) {
		t.Fatalf("redirect error = %v", err)
	}
	select {
	case request := <-targetRequests:
		t.Fatalf("redirect target received token-bearing request at %s", request.URL)
	default:
	}
}

func TestLegacyContractSendClassifiesStatusAndBoundsEveryResponse(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		limit   int64
		wantErr string
	}{
		{name: "ok", status: http.StatusOK, body: "ok", limit: 2},
		{name: "empty no content", status: http.StatusNoContent, limit: 1},
		{name: "redirect is failure", status: http.StatusMultipleChoices, body: "x", limit: 1, wantErr: "HTTP 300"},
		{name: "client error", status: http.StatusBadRequest, body: "x", limit: 1, wantErr: "HTTP 400"},
		{name: "oversized success", status: http.StatusOK, body: "123", limit: 2, wantErr: "exceeds 2 bytes"},
		{name: "oversized error", status: http.StatusBadGateway, body: "123", limit: 2, wantErr: "exceeds 2 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			client, err := NewClient(server.URL+"/services", "status-secret",
				WithHTTPClient(server.Client()), WithMaxResponseSize(test.limit))
			if err != nil {
				t.Fatal(err)
			}
			err = client.Send(context.Background(), Message{Text: "menu"})
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) || strings.Contains(err.Error(), "status-secret") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

type legacyContractSlackTransport func(*http.Request) (*http.Response, error)

func (transport legacyContractSlackTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestLegacyContractSendSanitizesTimeoutCancellationAndCredentialBearingFailures(t *testing.T) {
	const token = "webhook-token-must-not-leak"
	tests := []struct {
		name      string
		transport legacyContractSlackTransport
		want      string
	}{
		{
			name: "deadline",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("URL contained %s: %w", token, context.DeadlineExceeded)
			},
			want: "request timed out",
		},
		{
			name: "canceled",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("URL contained %s: %w", token, context.Canceled)
			},
			want: "request canceled",
		},
		{
			name: "transport",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("request to %s failed", token)
			},
			want: "request failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient("https://hooks.slack.example.test/services", token, WithHTTPClient(test.transport))
			if err != nil {
				t.Fatal(err)
			}
			err = client.Send(context.Background(), Message{Text: "menu"})
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), token) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
