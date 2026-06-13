// Package craftinginterpreters is the library behind the ci command line:
// the HTTP client, request shaping, and the typed data models for the
// Crafting Interpreters book at https://craftinginterpreters.com.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
package craftinginterpreters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultUserAgent identifies the client to craftinginterpreters. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "ci/dev (+https://github.com/tamnd/craftinginterpreters-cli)"

// ErrNotFound is returned when a resource cannot be found.
var ErrNotFound = errors.New("not found")

// Config holds constructor parameters.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://craftinginterpreters.com",
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Chapter is a single chapter in the Crafting Interpreters book.
type Chapter struct {
	Part  int    `json:"part"`
	Num   int    `json:"num"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Client talks to craftinginterpreters over HTTP.
type Client struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string
	rate       time.Duration
	retries    int
	mu         sync.Mutex
	last       time.Time
}

// NewClient returns a Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		userAgent:  cfg.UserAgent,
		rate:       cfg.Rate,
		retries:    cfg.Retries,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate <= 0 {
		return
	}
	if wait := c.rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// Contents fetches the table of contents and returns numbered chapters.
// It parses the HTML from /contents.html using only the strings stdlib.
// limit <= 0 returns all chapters.
func (c *Client) Contents(ctx context.Context, limit int) ([]Chapter, error) {
	rawURL := c.baseURL + "/contents.html"
	body, err := c.Get(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("contents: %w", err)
	}
	chapters := parseContents(string(body), c.baseURL)
	if limit > 0 && limit < len(chapters) {
		chapters = chapters[:limit]
	}
	return chapters, nil
}

// parseContents extracts numbered chapters from the contents HTML.
// It looks for lines matching:
//
//	<li><span class="num">N.</span><a href="slug.html">Title</a>
//
// and assigns part numbers by tracking <h2> section headers.
func parseContents(html, baseURL string) []Chapter {
	var chapters []Chapter
	part := 0
	num := 0

	lines := strings.Split(html, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Track part boundaries: <h2><span class="num">I.</span>...
		if strings.Contains(line, `<h2>`) && strings.Contains(line, `<span class="num">`) {
			numSpan := extractBetween(line, `<span class="num">`, `</span>`)
			// Roman numeral parts (I., II., III.) increment the part counter
			if numSpan == "I." || numSpan == "II." || numSpan == "III." || numSpan == "IV." {
				part++
			}
			continue
		}

		// Match numbered chapter entries: <li><span class="num">N.</span><a href="...">Title</a>
		if !strings.HasPrefix(line, `<li><span class="num">`) {
			continue
		}
		numStr := extractBetween(line, `<span class="num">`, `</span>`)
		if numStr == "" || numStr == "&nbsp;" {
			continue
		}
		// Only numbered chapters (ends with ".")
		if !strings.HasSuffix(numStr, ".") {
			continue
		}
		// Parse the chapter number
		chNum := 0
		_, _ = fmt.Sscanf(numStr, "%d.", &chNum)
		if chNum == 0 {
			continue
		}

		// Extract href and title from the <a> element
		href := extractAttr(line, "href")
		title := extractTagText(line, "a")
		if href == "" || title == "" {
			continue
		}
		// Skip design notes and anchors
		if strings.Contains(href, "#") {
			continue
		}

		num++
		url := baseURL + "/" + href
		chapters = append(chapters, Chapter{
			Part:  part,
			Num:   chNum,
			Title: title,
			URL:   url,
		})
	}
	return chapters
}

// Search filters Contents by query string (case-insensitive title match).
// limit <= 0 returns all matching chapters.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Chapter, error) {
	all, err := c.Contents(ctx, 0)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []Chapter
	for _, ch := range all {
		if strings.Contains(strings.ToLower(ch.Title), q) {
			out = append(out, ch)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// extractBetween returns the substring between start and end markers, or "".
func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return ""
	}
	return s[:j]
}

// extractAttr returns the value of the first occurrence of attr="..." in s.
func extractAttr(s, attr string) string {
	needle := attr + `="`
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	s = s[i+len(needle):]
	j := strings.Index(s, `"`)
	if j < 0 {
		return ""
	}
	return s[:j]
}

// extractTagText returns the text content of the first <tag>...</tag> in s.
func extractTagText(s, tag string) string {
	open := "<" + tag
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	// find end of opening tag
	s = s[i:]
	j := strings.Index(s, ">")
	if j < 0 {
		return ""
	}
	s = s[j+1:]
	close := "</" + tag + ">"
	k := strings.Index(s, close)
	if k < 0 {
		return ""
	}
	return strings.TrimSpace(s[:k])
}
