package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout      = 10 * time.Second
	defaultResponseSize = int64(64 << 10)
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Message struct {
	Channel   string `json:"channel"`
	Username  string `json:"username"`
	Text      string `json:"text"`
	IconEmoji string `json:"icon_emoji"`
}

type Client struct {
	endpoint        string
	httpClient      HTTPClient
	maxResponseSize int64
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

func NewClient(baseURL, token string, options ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("slack URL is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("slack token is required")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(token, "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("slack URL must be an absolute URL")
	}
	if parsed.Scheme != "https" {
		return nil, errors.New("slack URL must use HTTPS")
	}

	c := &Client{
		endpoint:        endpoint,
		httpClient:      newHTTPClient(nil),
		maxResponseSize: defaultResponseSize,
	}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

func newHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Client) Send(ctx context.Context, message Message) error {
	message.Channel = NormalizeChannel(message.Channel)
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Slack message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("create Slack request: invalid URL")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("send Slack message: request timed out")
		}
		if errors.Is(err, context.Canceled) {
			return errors.New("send Slack message: request canceled")
		}
		return errors.New("send Slack message: request failed")
	}
	defer resp.Body.Close()
	_, err = readResponse(resp.Body, c.maxResponseSize)
	if err != nil {
		return fmt.Errorf("read Slack response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Slack returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func NormalizeChannel(channel string) string {
	channel = strings.TrimSpace(channel)
	if channel != "" && !strings.HasPrefix(channel, "#") {
		return "#" + channel
	}
	return channel
}

func readResponse(reader io.Reader, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("body exceeds %d bytes", max)
	}
	return body, nil
}
