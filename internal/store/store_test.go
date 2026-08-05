package store

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/stores"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()

	index := Index{
		Frameworks: []IndexEntry{
			{
				Name:     "laravel",
				Label:    "Laravel",
				Versions: []string{"11", "10"},
				Latest:   "11",
				Detect: []config.FrameworkRule{
					{File: "artisan"},
					{Composer: "laravel/framework"},
				},
			},
			{
				Name:     "symfony",
				Label:    "Symfony",
				Versions: []string{"7", "6"},
				Latest:   "7",
				Detect: []config.FrameworkRule{
					{Composer: "symfony/framework-bundle"},
				},
			},
		},
	}

	laravelYAML := `name: laravel
label: Laravel
version: "11"
public_dir: public
detect:
  - file: artisan
  - composer: laravel/framework
console: artisan
`

	symfonyYAML := `name: symfony
label: Symfony
version: "7"
public_dir: public
detect:
  - composer: symfony/framework-bundle
console: bin/console
`

	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(index)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck
	})
	mux.HandleFunc("/laravel/11.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(laravelYAML)) //nolint:errcheck
	})
	mux.HandleFunc("/symfony/7.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(symfonyYAML)) //nolint:errcheck
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return httptest.NewServer(mux)
}

func testClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return &Client{
		BaseURL: srv.URL,
	}
}

// When the primary base returns a non-200, the client must transparently fetch
// from the fallback base.
func TestFetchIndex_FallsBackWhenPrimaryFails(t *testing.T) {
	good := testServer(t)
	defer good.Close()

	c := &Client{
		BaseURL:   good.URL + "/missing", // /missing/index.json -> 404
		Fallbacks: []string{good.URL},
	}
	idx, err := c.FetchIndex()
	if err != nil {
		t.Fatalf("FetchIndex should succeed via fallback: %v", err)
	}
	if len(idx.Frameworks) == 0 || idx.Frameworks[0].Name != "laravel" {
		t.Fatalf("unexpected index from fallback: %+v", idx)
	}
}

// A droplet with no route to the store is the ordinary case, not the exception:
// the repository is private, so raw.githubusercontent.com answers 404 until it
// is public. Every base failing therefore falls through to the copy embedded in
// this binary rather than failing the call.
func TestFetchIndex_FallsBackToTheEmbeddedStore(t *testing.T) {
	good := testServer(t)
	dead := good.URL
	good.Close() // now refuses connections

	c := &Client{
		BaseURL:   dead,
		Fallbacks: []string{dead + "/also-dead"},
		Embedded:  stores.Frameworks,
	}
	idx, err := c.FetchIndex()
	if err != nil {
		t.Fatalf("FetchIndex should fall back to the embedded store: %v", err)
	}
	if len(idx.Frameworks) == 0 {
		t.Fatal("the embedded framework index is empty")
	}
}

// A client with no embedded store behind it (a mirror configured by hand, say)
// still reports the failure rather than pretending to have answered.
func TestFetchIndex_AllBasesFailWithNoEmbeddedStore(t *testing.T) {
	good := testServer(t)
	dead := good.URL
	good.Close()

	c := &Client{BaseURL: dead, Fallbacks: []string{dead + "/also-dead"}}
	if _, err := c.FetchIndex(); err == nil {
		t.Fatal("expected an error when all bases are unreachable")
	}
}

// A definition this binary shipped is served from the embedded store when the
// network cannot answer, so a fresh install has every framework it was built
// with whether or not it can reach GitHub.
func TestFetchFramework_FallsBackToTheEmbeddedStore(t *testing.T) {
	good := testServer(t)
	dead := good.URL
	good.Close()

	c := &Client{BaseURL: dead, Embedded: stores.Frameworks}
	fw, err := c.FetchFramework("laravel", "13")
	if err != nil {
		t.Fatalf("FetchFramework from the embedded store: %v", err)
	}
	if fw.Name != "laravel" {
		t.Errorf("framework name = %q, want laravel", fw.Name)
	}
}

// NewClient points the framework store at this repository. The definitions are
// authored here now, so the fetch and the embedded copy resolve the same paths.
func TestNewClient_UsesTheInRepoStore(t *testing.T) {
	c := NewClient()
	if !strings.Contains(c.BaseURL, "realrashid/servlo") {
		t.Errorf("primary store URL = %q, want realrashid/servlo", c.BaseURL)
	}
	if !strings.HasSuffix(c.BaseURL, "/stores/frameworks") {
		t.Errorf("store URL = %q, want it to end at the frameworks store", c.BaseURL)
	}
	if c.Embedded != stores.Frameworks {
		t.Errorf("client must carry the embedded framework store, got %q", c.Embedded)
	}
}

// NewServiceClient points at the same repository's services store.
func TestNewServiceClient_UsesTheInRepoStore(t *testing.T) {
	c := NewServiceClient()
	if !strings.HasSuffix(c.BaseURL, "/stores/services") {
		t.Errorf("primary service-store URL = %q, want the in-repo services store", c.BaseURL)
	}
	if c.Embedded != stores.Services {
		t.Errorf("service client must carry the embedded service store, got %q", c.Embedded)
	}
}

// ── FetchIndex ───────────────────────────────────────────────────────────────

func TestFetchIndex(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	idx, err := c.FetchIndex()
	if err != nil {
		t.Fatalf("FetchIndex() error: %v", err)
	}
	if len(idx.Frameworks) != 2 {
		t.Fatalf("expected 2 frameworks, got %d", len(idx.Frameworks))
	}
	if idx.Frameworks[0].Name != "laravel" {
		t.Errorf("expected first framework to be laravel, got %q", idx.Frameworks[0].Name)
	}
}

// ── FetchFramework ───────────────────────────────────────────────────────────

func TestFetchFramework(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	fw, err := c.FetchFramework("laravel", "11")
	if err != nil {
		t.Fatalf("FetchFramework() error: %v", err)
	}
	if fw.Name != "laravel" {
		t.Errorf("expected name=laravel, got %q", fw.Name)
	}
	if fw.Version != "11" {
		t.Errorf("expected version=11, got %q", fw.Version)
	}
	if fw.Console != "artisan" {
		t.Errorf("expected console=artisan, got %q", fw.Console)
	}
}

func TestFetchFramework_ResolvesLatest(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	fw, err := c.FetchFramework("laravel", "")
	if err != nil {
		t.Fatalf("FetchFramework() error: %v", err)
	}
	if fw.Version != "11" {
		t.Errorf("expected latest version=11, got %q", fw.Version)
	}
}

func TestFetchFramework_NotFound(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	_, err := c.FetchFramework("nonexistent", "1")
	if err == nil {
		t.Fatal("expected error for nonexistent framework")
	}
}

func TestFetchFramework_AlwaysFresh(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	// Fetch should always hit the server (no local cache).
	fw, err := c.FetchFramework("symfony", "7")
	if err != nil {
		t.Fatalf("FetchFramework() error: %v", err)
	}
	if fw.Name != "symfony" {
		t.Errorf("expected name=symfony, got %q", fw.Name)
	}

	// Stop server — second call should fail (no cache fallback).
	srv.Close()
	_, err = c.FetchFramework("symfony", "7")
	if err == nil {
		t.Error("expected error when server is down, got nil")
	}
}

// ── Search ───────────────────────────────────────────────────────────────────

func TestSearch(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	results, err := c.Search("sym")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "symfony" {
		t.Errorf("expected symfony, got %q", results[0].Name)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	results, err := c.Search("LARAVEL")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "laravel" {
		t.Errorf("expected laravel from case-insensitive search, got %v", results)
	}
}

func TestSearch_NoMatch(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	results, err := c.Search("django")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// ── DetectFromStore ──────────────────────────────────────────────────────────

func TestDetectFromStore_FileRule(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	dir := t.TempDir()
	// Create artisan file to trigger Laravel detection
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0o644) //nolint:errcheck

	entry, version, ok := c.DetectFromStore(dir)
	if !ok {
		t.Fatal("expected detection to succeed")
	}
	if entry.Name != "laravel" {
		t.Errorf("expected laravel, got %q", entry.Name)
	}
	if version != "11" {
		t.Errorf("expected version=11 (latest), got %q", version)
	}
}

func TestDetectFromStore_NoMatch(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()
	c := testClient(t, srv)

	dir := t.TempDir()
	_, _, ok := c.DetectFromStore(dir)
	if ok {
		t.Fatal("expected no detection in empty dir")
	}
}

func TestFetchWithRetry_RecoversFromTransientFailure(t *testing.T) {
	prevSleep := sleepFn
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { sleepFn = prevSleep })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway) // 502, transient
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	body, err := fetchWithRetry(&http.Client{Timeout: httpTimeout}, srv.URL)
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestFetchWithRetry_DoesNotRetry404(t *testing.T) {
	prevSleep := sleepFn
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { sleepFn = prevSleep })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchWithRetry(&http.Client{Timeout: httpTimeout}, srv.URL); err == nil {
		t.Fatal("expected error for 404")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("a 4xx must not be retried, got %d attempts", got)
	}
}

func TestFetchWithRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	prevSleep := sleepFn
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { sleepFn = prevSleep })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchWithRetry(&http.Client{Timeout: httpTimeout}, srv.URL); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&calls); got != maxFetchAttempts {
		t.Fatalf("expected %d attempts, got %d", maxFetchAttempts, got)
	}
}
