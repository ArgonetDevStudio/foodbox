package crawler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
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

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Client struct {
	listURL      *url.URL
	origin       string
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
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("crawler URL must be an absolute URL")
	}
	if parsed.Scheme != "https" {
		return nil, errors.New("crawler URL must use HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("crawler URL must not contain credentials")
	}
	if isLocalHost(parsed.Hostname()) {
		return nil, errors.New("crawler URL must use a public host")
	}

	c := &Client{
		listURL:      parsed,
		origin:       canonicalOrigin(parsed),
		httpClient:   newHTTPClient(net.DefaultResolver),
		maxHTMLSize:  defaultMaxHTMLSize,
		maxImageSize: defaultMaxImageSize,
		now: func() time.Time {
			return time.Now().In(koreaLocation)
		},
	}
	for _, option := range options {
		option(c)
	}
	c.httpClient = c.withRedirectValidation(c.httpClient)
	return c, nil
}

func newHTTPClient(resolver ipResolver) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = publicDialContext(resolver, &net.Dialer{
		Timeout:   defaultTimeout,
		KeepAlive: 30 * time.Second,
	})
	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}
}

func publicDialContext(resolver ipResolver, dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("crawler destination address is invalid")
		}
		addresses, err := resolvePublicAddresses(ctx, resolver, host)
		if err != nil {
			return nil, err
		}

		var lastErr error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, errors.New("crawler destination is unavailable")
		}
		return nil, errors.New("crawler destination did not resolve")
	}
}

func resolvePublicAddresses(ctx context.Context, resolver ipResolver, host string) ([]net.IPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		if isLocalHost(ip.String()) {
			return nil, errors.New("crawler destination must be public")
		}
		return []net.IPAddr{{IP: ip}}, nil
	}

	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("crawler destination did not resolve")
	}
	for _, address := range addresses {
		if address.IP == nil || isLocalHost(address.IP.String()) {
			return nil, errors.New("crawler destination must be public")
		}
	}
	return addresses, nil
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
	return c.resolveURL(c.listURL, href)
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
	return c.resolveURL(detailURL, imageRef)
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
	if err := c.validateURL(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, errors.New("create crawler request: invalid URL")
	}
	req.Header.Set("User-Agent", "foodbox/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("crawler request failed")
	}
	if resp.Request != nil {
		if err := c.validateURL(resp.Request.URL); err != nil {
			resp.Body.Close()
			return nil, errors.New("crawler response origin rejected")
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) resolveURL(base *url.URL, reference string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return nil, errors.New("invalid URL in menu page")
	}
	resolved := base.ResolveReference(parsed)
	if err := c.validateURL(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func (c *Client) validateURL(target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.Host == "" || target.Hostname() == "" {
		return errors.New("menu page URL must use HTTPS")
	}
	if target.User != nil {
		return errors.New("menu page URL must not contain credentials")
	}
	if isLocalHost(target.Hostname()) || canonicalOrigin(target) != c.origin {
		return errors.New("menu page URL must use the configured crawler origin")
	}
	return nil
}

func (c *Client) withRedirectValidation(client HTTPClient) HTTPClient {
	httpClient, ok := client.(*http.Client)
	if !ok {
		return client
	}
	clone := *httpClient
	previous := httpClient.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if err := c.validateURL(request.URL); err != nil {
			return errors.New("crawler redirect rejected")
		}
		if previous != nil {
			return previous(request, via)
		}
		if len(via) >= 10 {
			return errors.New("crawler redirect limit exceeded")
		}
		return nil
	}
	return &clone
}

func canonicalOrigin(target *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	port := target.Port()
	if port == "" {
		port = "443"
	}
	return "https://" + net.JoinHostPort(host, port)
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || !ip.IsGlobalUnicast() {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
