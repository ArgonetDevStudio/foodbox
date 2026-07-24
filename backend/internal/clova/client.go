package clova

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultTimeout      = 15 * time.Second
	defaultResponseSize = int64(16 << 20)
	defaultImageSize    = int64(10 << 20)
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	endpoint        string
	secret          string
	httpClient      HTTPClient
	maxResponseSize int64
	maxImageSize    int64
	now             func() time.Time
}

type Option func(*Client)

func WithHTTPClient(client HTTPClient) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithMaxResponseSize(size int64) Option {
	return func(c *Client) {
		if size > 0 {
			c.maxResponseSize = size
		}
	}
}

func WithMaxImageSize(size int64) Option {
	return func(c *Client) {
		if size > 0 {
			c.maxImageSize = size
		}
	}
}

func withClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

func NewClient(endpoint, secret string, options ...Option) (*Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("clova endpoint must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("clova endpoint must use HTTP or HTTPS")
	}
	if endpoint == "" {
		return nil, errors.New("clova endpoint is required")
	}
	if secret == "" {
		return nil, errors.New("clova secret is required")
	}

	c := &Client{
		endpoint: endpoint,
		secret:   secret,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		maxResponseSize: defaultResponseSize,
		maxImageSize:    defaultImageSize,
		now:             time.Now,
	}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

type requestBody struct {
	Images     []requestImage `json:"images"`
	Lang       string         `json:"lang"`
	RequestID  string         `json:"requestId"`
	ResultType string         `json:"resultType"`
	Timestamp  int64          `json:"timestamp"`
	Version    string         `json:"version"`
}

type requestImage struct {
	Format string `json:"format"`
	Name   string `json:"name"`
	Data   string `json:"data"`
}

type Response struct {
	Version   string          `json:"version"`
	RequestID string          `json:"requestId"`
	Timestamp int64           `json:"timestamp"`
	Images    []ResponseImage `json:"images"`
}

type ResponseImage struct {
	UID              string           `json:"uid"`
	Name             string           `json:"name"`
	InferResult      string           `json:"inferResult"`
	Message          string           `json:"message"`
	ValidationResult ValidationResult `json:"validationResult"`
	Fields           []Field          `json:"fields"`
}

type ValidationResult struct {
	Result string `json:"result"`
}

type Field struct {
	ValueType       string       `json:"valueType"`
	BoundingPoly    BoundingPoly `json:"boundingPoly"`
	InferText       string       `json:"inferText"`
	InferConfidence float64      `json:"inferConfidence"`
}

type BoundingPoly struct {
	Vertices []Vertex `json:"vertices"`
}

type Vertex struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (c *Client) Recognize(ctx context.Context, image []byte) (*Response, []byte, error) {
	if int64(len(image)) > c.maxImageSize {
		return nil, nil, fmt.Errorf("clova image exceeds %d bytes", c.maxImageSize)
	}
	format, err := imageFormat(image)
	if err != nil {
		return nil, nil, err
	}

	payload := requestBody{
		Images: []requestImage{{
			Format: format,
			Name:   "menu",
			Data:   base64.StdEncoding.EncodeToString(image),
		}},
		Lang:       "ko",
		RequestID:  "string",
		ResultType: "string",
		Timestamp:  c.now().UnixMilli(),
		Version:    "V1",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode clova request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("create clova request: invalid endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OCR-SECRET", c.secret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, errors.New("send clova request: request timed out")
		}
		if errors.Is(err, context.Canceled) {
			return nil, nil, errors.New("send clova request: request canceled")
		}
		return nil, nil, errors.New("send clova request: request failed")
	}
	defer resp.Body.Close()

	raw, err := readLimited(resp.Body, c.maxResponseSize)
	if err != nil {
		return nil, nil, fmt.Errorf("read clova response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, raw, fmt.Errorf("clova returned HTTP %d", resp.StatusCode)
	}

	var result Response
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, raw, fmt.Errorf("decode clova response: %w", err)
	}
	return &result, raw, nil
}

func imageFormat(image []byte) (string, error) {
	if len(image) >= 8 && bytes.Equal(image[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "png", nil
	}
	if len(image) >= 3 && image[0] == 0xff && image[1] == 0xd8 && image[2] == 0xff {
		return "jpg", nil
	}
	return "", errors.New("clova only supports JPEG and PNG images")
}

func readLimited(reader io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(reader, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("body exceeds %d bytes", max)
	}
	return body, nil
}
