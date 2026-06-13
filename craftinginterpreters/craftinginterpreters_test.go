package craftinginterpreters_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tamnd/craftinginterpreters-cli/craftinginterpreters"
)

// minimalContentsHTML is a trimmed replica of the real contents page structure.
const minimalContentsHTML = `<!DOCTYPE html>
<html>
<body>
<div class="toc">
  <h2><span class="num">I.</span><a href="welcome.html" name="welcome">Welcome</a></h2>
  <ul>
    <li><span class="num">1.</span><a href="introduction.html">Introduction</a>
    </li>
    <li class="design-note">
    <span class="num">&nbsp;</span><a href="introduction.html#design-note">Design Note: What&#39;s in a Name?</a>
    </li>
    <li><span class="num">2.</span><a href="a-map-of-the-territory.html">A Map of the Territory</a>
    </li>
    <li><span class="num">3.</span><a href="the-lox-language.html">The Lox Language</a>
    </li>
  </ul>
  <h2><span class="num">II.</span><a href="a-tree-walk-interpreter.html" name="a-tree-walk-interpreter">A Tree-Walk Interpreter</a></h2>
  <ul>
    <li><span class="num">4.</span><a href="scanning.html">Scanning</a>
    </li>
    <li><span class="num">5.</span><a href="representing-code.html">Representing Code</a>
    </li>
  </ul>
</div>
</body>
</html>`

func newTestServer(t *testing.T, body string) (*httptest.Server, *craftinginterpreters.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	cfg := craftinginterpreters.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := craftinginterpreters.NewClient(cfg)
	return srv, c
}

func TestContents(t *testing.T) {
	_, c := newTestServer(t, minimalContentsHTML)

	chapters, err := c.Contents(context.Background(), 0)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(chapters) != 5 {
		t.Fatalf("got %d chapters, want 5", len(chapters))
	}

	if chapters[0].Num != 1 {
		t.Errorf("chapter[0].Num = %d, want 1", chapters[0].Num)
	}
	if chapters[0].Title != "Introduction" {
		t.Errorf("chapter[0].Title = %q, want Introduction", chapters[0].Title)
	}
	if chapters[0].Part != 1 {
		t.Errorf("chapter[0].Part = %d, want 1", chapters[0].Part)
	}
	if chapters[3].Num != 4 {
		t.Errorf("chapter[3].Num = %d, want 4", chapters[3].Num)
	}
	if chapters[3].Part != 2 {
		t.Errorf("chapter[3].Part = %d, want 2", chapters[3].Part)
	}
}

func TestContentsLimit(t *testing.T) {
	_, c := newTestServer(t, minimalContentsHTML)

	chapters, err := c.Contents(context.Background(), 2)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters with limit=2, want 2", len(chapters))
	}
}

func TestContentsURLs(t *testing.T) {
	srv, c := newTestServer(t, minimalContentsHTML)

	chapters, err := c.Contents(context.Background(), 0)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	for _, ch := range chapters {
		if !strings.HasPrefix(ch.URL, srv.URL) {
			t.Errorf("chapter %q URL %q does not start with base URL %q", ch.Title, ch.URL, srv.URL)
		}
		if !strings.HasSuffix(ch.URL, ".html") {
			t.Errorf("chapter %q URL %q does not end with .html", ch.Title, ch.URL)
		}
	}
}

func TestSearch(t *testing.T) {
	_, c := newTestServer(t, minimalContentsHTML)

	hits, err := c.Search(context.Background(), "scan", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search(scan): got %d hits, want 1", len(hits))
	}
	if hits[0].Title != "Scanning" {
		t.Errorf("hit title = %q, want Scanning", hits[0].Title)
	}
}

func TestSearchLimit(t *testing.T) {
	_, c := newTestServer(t, minimalContentsHTML)

	hits, err := c.Search(context.Background(), "a", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 2 {
		t.Errorf("Search with limit=2 returned %d hits", len(hits))
	}
}

func TestSearchNoResults(t *testing.T) {
	_, c := newTestServer(t, minimalContentsHTML)

	hits, err := c.Search(context.Background(), "zzznomatch", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for no-match query, got %d", len(hits))
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	cfg := craftinginterpreters.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := craftinginterpreters.NewClient(cfg)

	body, err := c.Get(context.Background(), srv.URL+"/test")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}
