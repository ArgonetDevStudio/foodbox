package clova

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLegacyContractRecognizeSendsExactV1HTTPExchange(t *testing.T) {
	fixedTime := time.Date(2026, time.July, 25, 9, 0, 0, 123_000_000, time.FixedZone("KST", 9*60*60))
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0xff}
	wantBody := []byte(`{"images":[{"format":"png","name":"menu","data":"iVBORw0KGgoA/w=="}],"lang":"ko","requestId":"string","resultType":"string","timestamp":1784937600123,"version":"V1"}`)
	wantResponse := []byte(`{"version":"V1","requestId":"legacy-response","timestamp":1784937600123,"images":[]}`)

	requests := make(chan []byte, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if request.URL.RequestURI() != "/custom/ocr?template=legacy" {
			t.Errorf("request URI = %q", request.URL.RequestURI())
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := request.Header.Get("X-OCR-SECRET"); got != "clova-contract-secret" {
			t.Errorf("X-OCR-SECRET = %q", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requests <- body
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(wantResponse)
	}))
	defer server.Close()

	client, err := NewClient(
		server.URL+"/custom/ocr?template=legacy",
		"clova-contract-secret",
		WithHTTPClient(server.Client()),
		withClock(func() time.Time { return fixedTime }),
	)
	if err != nil {
		t.Fatal(err)
	}
	response, raw, err := client.Recognize(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-requests; !bytes.Equal(got, wantBody) {
		t.Fatalf("request body = %s\nwant = %s", got, wantBody)
	}
	if !bytes.Equal(raw, wantResponse) {
		t.Fatalf("raw response = %s", raw)
	}
	if response.Version != "V1" || response.RequestID != "legacy-response" || response.Timestamp != fixedTime.UnixMilli() {
		t.Fatalf("decoded response = %+v", response)
	}
}

func TestLegacyContractRecognizeImageAndResponseLimitsAreInclusive(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0x01}
	responseBody := []byte(`{}`)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = writer.Write(responseBody)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret",
		WithHTTPClient(server.Client()),
		WithMaxImageSize(int64(len(image))),
		WithMaxResponseSize(int64(len(responseBody))),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, raw, err := client.Recognize(context.Background(), image); err != nil || !bytes.Equal(raw, responseBody) {
		t.Fatalf("exact-limit request: raw = %q, error = %v", raw, err)
	}

	oversized := append(append([]byte(nil), image...), 0x02)
	if _, _, err := client.Recognize(context.Background(), oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized image error = %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, oversized input must fail before HTTP", got)
	}

	responseLimited, err := NewClient(server.URL, "secret",
		WithHTTPClient(server.Client()),
		WithMaxResponseSize(int64(len(responseBody)-1)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, raw, err := responseLimited.Recognize(context.Background(), image); err == nil || raw != nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response: raw = %q, error = %v", raw, err)
	}
}

func TestLegacyContractRecognizeClassifiesStatusAndReturnsBoundedRawErrorBody(t *testing.T) {
	const secret = "status-secret-must-not-leak"
	responseBody := []byte(`{"version":"V1","images":[]}`)
	tests := []struct {
		name      string
		status    int
		wantError string
	}{
		{name: "success", status: http.StatusOK},
		{name: "last 2xx", status: 299},
		{name: "redirect", status: http.StatusMultipleChoices, wantError: "HTTP 300"},
		{name: "unauthorized", status: http.StatusUnauthorized, wantError: "HTTP 401"},
		{name: "server failure", status: http.StatusBadGateway, wantError: "HTTP 502"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write(responseBody)
			}))
			defer server.Close()

			client, err := NewClient(server.URL, secret,
				WithHTTPClient(server.Client()),
				WithMaxResponseSize(int64(len(responseBody))),
			)
			if err != nil {
				t.Fatal(err)
			}
			response, raw, err := client.Recognize(context.Background(), []byte{0xff, 0xd8, 0xff})
			if !bytes.Equal(raw, responseBody) {
				t.Fatalf("raw = %q", raw)
			}
			if test.wantError == "" {
				if err != nil || response == nil {
					t.Fatalf("response = %+v, error = %v", response, err)
				}
				return
			}
			if err == nil || response != nil || !strings.Contains(err.Error(), test.wantError) || strings.Contains(err.Error(), secret) {
				t.Fatalf("response = %+v, error = %v", response, err)
			}
		})
	}
}

func TestLegacyContractClovaDefaultClientHasBoundedTimeoutAndRejectsRedirects(t *testing.T) {
	client := newHTTPClient(http.DefaultTransport)
	if defaultTimeout != 15*time.Second {
		t.Fatalf("contract timeout constant = %s", defaultTimeout)
	}
	if client.Timeout != defaultTimeout {
		t.Fatalf("timeout = %s", client.Timeout)
	}

	request, err := http.NewRequest(http.MethodPost, "https://clova.example.test/ocr", nil)
	if err != nil {
		t.Fatal(err)
	}
	redirectErr := client.CheckRedirect(request, []*http.Request{request})
	if !errors.Is(redirectErr, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error = %v", redirectErr)
	}
}

func TestLegacyContractRecognizeRedirectNeverForwardsOCRSecret(t *testing.T) {
	const secret = "never-forward-clova-secret"
	targetRequests := make(chan *http.Request, 1)
	target := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		targetRequests <- request.Clone(context.Background())
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-OCR-SECRET"); got != secret {
			t.Errorf("source secret = %q", got)
		}
		writer.Header().Set("Location", target.URL+"/stolen")
		writer.WriteHeader(http.StatusFound)
	}))
	defer source.Close()

	httpClient := newHTTPClient(source.Client().Transport)
	client, err := NewClient(source.URL+"/ocr", secret, WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.Recognize(context.Background(), []byte{0xff, 0xd8, 0xff})
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), secret) {
		t.Fatalf("redirect error = %v", err)
	}
	select {
	case request := <-targetRequests:
		t.Fatalf("redirect target received %s with secret %q", request.URL, request.Header.Get("X-OCR-SECRET"))
	default:
	}
}

type legacyContractClovaTransport func(*http.Request) (*http.Response, error)

func (transport legacyContractClovaTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestLegacyContractRecognizeSanitizesTimeoutAndCredentialBearingFailures(t *testing.T) {
	const secret = "clova-secret-must-not-leak"
	tests := []struct {
		name      string
		transport legacyContractClovaTransport
		want      string
	}{
		{
			name: "deadline",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("endpoint contained %s: %w", secret, context.DeadlineExceeded)
			},
			want: "request timed out",
		},
		{
			name: "transport",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("request header contained %s", secret)
			},
			want: "request failed",
		},
		{
			name: "canceled",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("endpoint contained %s: %w", secret, context.Canceled)
			},
			want: "request canceled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient("https://clova.example.test/ocr", secret, WithHTTPClient(test.transport))
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.Recognize(context.Background(), []byte{0xff, 0xd8, 0xff})
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), secret) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
