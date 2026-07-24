package clova

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecognizeSendsV1RequestAndReturnsTypedAndRawResponse(t *testing.T) {
	fixedTime := time.Date(2026, 7, 25, 1, 2, 3, 0, time.UTC)
	image := append([]byte{0xff, 0xd8, 0xff}, []byte("jpeg")...)
	responseJSON := `{"version":"V1","requestId":"req","timestamp":1,"images":[{"inferResult":"SUCCESS","fields":[{"inferText":"rice","inferConfidence":0.99}]}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("X-OCR-SECRET"); got != "top-secret" {
			t.Errorf("secret header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}

		var request requestBody
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Version != "V1" || request.Lang != "ko" || request.Timestamp != fixedTime.UnixMilli() {
			t.Errorf("unexpected request metadata: %+v", request)
		}
		if len(request.Images) != 1 || request.Images[0].Format != "jpg" || request.Images[0].Data != base64.StdEncoding.EncodeToString(image) {
			t.Errorf("unexpected image: %+v", request.Images)
		}
		_, _ = io.WriteString(w, responseJSON)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "top-secret", withClock(func() time.Time { return fixedTime }))
	if err != nil {
		t.Fatal(err)
	}
	response, raw, err := client.Recognize(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != responseJSON {
		t.Fatalf("raw = %s", raw)
	}
	if got := response.Images[0].Fields[0].InferText; got != "rice" {
		t.Fatalf("infer text = %q", got)
	}
}

func TestRecognizeDetectsPNG(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("data")...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request requestBody
		_ = json.NewDecoder(r.Body).Decode(&request)
		if got := request.Images[0].Format; got != "png" {
			t.Errorf("format = %q", got)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "secret")
	if _, _, err := client.Recognize(context.Background(), png); err != nil {
		t.Fatal(err)
	}
}

func TestRecognizeRejectsUnsupportedImageBeforeRequest(t *testing.T) {
	client, _ := NewClient("http://unused.invalid", "secret")
	if _, _, err := client.Recognize(context.Background(), []byte("gif")); err == nil {
		t.Fatal("expected error")
	}
}

func TestRecognizeReturnsStatusWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "denied")
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "never-print-me")
	_, raw, err := client.Recognize(context.Background(), []byte{0xff, 0xd8, 0xff})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "never-print-me") || string(raw) != "denied" {
		t.Fatalf("unsafe error or wrong raw response: %v %q", err, raw)
	}
}

func TestRecognizeLimitsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "12345")
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "secret", WithMaxResponseSize(4))
	if _, _, err := client.Recognize(context.Background(), []byte{0xff, 0xd8, 0xff}); err == nil {
		t.Fatal("expected response size error")
	}
}

func TestRecognizeLimitsImageBeforeRequest(t *testing.T) {
	client, _ := NewClient("http://unused.invalid", "secret", WithMaxImageSize(3))
	image := []byte{0xff, 0xd8, 0xff, 0x00}
	if _, _, err := client.Recognize(context.Background(), image); err == nil {
		t.Fatal("expected image size error")
	}
}

type leakingTransport struct{}

func (leakingTransport) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("secret")
}

func TestRecognizeSanitizesTransportError(t *testing.T) {
	client, _ := NewClient("https://example.com/ocr", "secret", WithHTTPClient(leakingTransport{}))
	_, _, err := client.Recognize(context.Background(), []byte{0xff, 0xd8, 0xff})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}
