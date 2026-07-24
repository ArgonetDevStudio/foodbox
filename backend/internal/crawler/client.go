package crawler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	defaultTimeout       = 15 * time.Second
	defaultMaxHTMLSize   = int64(4 << 20)
	defaultMaxImageSize  = int64(20 << 20)
	defaultMenuTitleTerm = "식단표"
)

var koreaLocation = time.FixedZone("Asia/Seoul", 9*60*60)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	listURL      *url.URL
	httpClient   HTTPClient
	maxHTMLSize  int64
	maxImageSize int64
	now          func() time.Time
}

type Option func(*Client)

func WithHTTPClient(client HTTPClient) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithLimits(maxHTMLSize, maxImageSize int64) Option {
	return func(c *Client) {
		if maxHTMLSize > 0 {
			c.maxHTMLSize = maxHTMLSize
		}
		if maxImageSize > 0 {
			c.maxImageSize = maxImageSize
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

func NewClient(listURL string, options ...Option) (*Client, error) {
	parsed, err := url.Parse(listURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("crawler URL must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("crawler URL must use HTTP or HTTPS")
	}

	c := &Client{
		listURL: parsed,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		maxHTMLSize:  defaultMaxHTMLSize,
		maxImageSize: defaultMaxImageSize,
		now: func() time.Time {
			return time.Now().In(koreaLocation)
		},
	}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

// Download saves the current month's menu image to a temporary file. The
// caller owns the returned file and must remove it when processing finishes.
func (c *Client) Download(ctx context.Context) (string, error) {
	detailURL, err := c.findCurrentDetailURL(ctx, c.now())
	if err != nil {
		return "", err
	}
	imageURL, err := c.findImageURL(ctx, detailURL)
	if err != nil {
		return "", err
	}
	return c.downloadImage(ctx, imageURL)
}

func (c *Client) findCurrentDetailURL(ctx context.Context, now time.Time) (*url.URL, error) {
	document, err := c.fetchDocument(ctx, c.listURL)
	if err != nil {
		return nil, fmt.Errorf("fetch menu list: %w", err)
	}

	month := now.Format("2006년 01월")
	var href string
	document.Find(".bbs-list a.aline").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		title := strings.Join(strings.Fields(selection.Text()), " ")
		if strings.Contains(title, month) && strings.Contains(title, defaultMenuTitleTerm) {
			href, _ = selection.Attr("href")
			return false
		}
		return true
	})
	if strings.TrimSpace(href) == "" {
		return nil, fmt.Errorf("menu link not found for %s", month)
	}
	return resolveURL(c.listURL, href)
}

func (c *Client) findImageURL(ctx context.Context, detailURL *url.URL) (*url.URL, error) {
	document, err := c.fetchDocument(ctx, detailURL)
	if err != nil {
		return nil, fmt.Errorf("fetch menu detail: %w", err)
	}

	content := document.Find("#bo_v_con").First()
	if content.Length() == 0 {
		return nil, errors.New("menu detail does not contain #bo_v_con")
	}
	imageRef, _ := content.Find("img").First().Attr("src")
	if strings.TrimSpace(imageRef) == "" {
		imageRef, _ = content.Find("a").First().Attr("href")
	}
	if strings.TrimSpace(imageRef) == "" {
		return nil, errors.New("menu image not found in #bo_v_con")
	}
	return resolveURL(detailURL, imageRef)
}

func (c *Client) fetchDocument(ctx context.Context, target *url.URL) (*goquery.Document, error) {
	resp, err := c.get(ctx, target)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := io.LimitReader(resp.Body, c.maxHTMLSize+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read HTML: %w", err)
	}
	if int64(len(body)) > c.maxHTMLSize {
		return nil, fmt.Errorf("HTML exceeds %d bytes", c.maxHTMLSize)
	}
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}
	return document, nil
}

func (c *Client) downloadImage(ctx context.Context, imageURL *url.URL) (path string, resultErr error) {
	resp, err := c.get(ctx, imageURL)
	if err != nil {
		return "", fmt.Errorf("download menu image: %w", err)
	}
	defer resp.Body.Close()
	if resp.ContentLength > c.maxImageSize {
		return "", fmt.Errorf("menu image exceeds %d bytes", c.maxImageSize)
	}

	file, err := os.CreateTemp("", "foodbox-menu-*")
	if err != nil {
		return "", fmt.Errorf("create menu image temporary file: %w", err)
	}
	path = file.Name()
	keep := false
	defer func() {
		if closeErr := file.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close menu image: %w", closeErr)
		}
		if resultErr != nil || !keep {
			_ = os.Remove(path)
			path = ""
		}
	}()

	written, err := io.Copy(file, io.LimitReader(resp.Body, c.maxImageSize+1))
	if err != nil {
		return path, fmt.Errorf("write menu image: %w", err)
	}
	if written > c.maxImageSize {
		return path, fmt.Errorf("menu image exceeds %d bytes", c.maxImageSize)
	}
	if err := file.Sync(); err != nil {
		return path, fmt.Errorf("sync menu image: %w", err)
	}
	keep = true
	return path, nil
}

func (c *Client) get(ctx context.Context, target *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, errors.New("create crawler request: invalid URL")
	}
	req.Header.Set("User-Agent", "foodbox/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func resolveURL(base *url.URL, reference string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return nil, errors.New("invalid URL in menu page")
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return nil, errors.New("menu page URL must use HTTP or HTTPS")
	}
	return resolved, nil
}
