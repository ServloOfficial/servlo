package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpxProxy_DisablesCaching(t *testing.T) {
	p := newSpxProxy()
	if p.ModifyResponse == nil {
		t.Fatal("newSpxProxy must set ModifyResponse so SPX responses are not cached")
	}
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("ETag", "abc")
	if err := p.ModifyResponse(resp); err != nil {
		t.Fatalf("ModifyResponse: %v", err)
	}
	// SPX sends no cache headers, so the browser heuristically caches the
	// report list — a stale empty list. The proxy must forbid caching.
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestSpxStripPrefix(t *testing.T) {
	cases := map[string]string{
		"/_spx/":    "/",
		"/_spx":     "/",
		"/_spx/foo": "/foo",
		"/_spx/a/b": "/a/b",
	}
	for in, want := range cases {
		if got := spxStripPrefix(in); got != want {
			t.Errorf("spxStripPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpxProxyDirector_RewritesHostStripsPrefixAndForwardedHost(t *testing.T) {
	req := httptest.NewRequest("GET", "http://lerd.localhost/_spx/?SPX_UI_URI=/", nil)
	req.Header.Set("X-Forwarded-Host", "evil.example")

	newSpxProxy().Director(req)

	if req.Host != "profiler.localhost" {
		t.Errorf("Host = %q, want profiler.localhost", req.Host)
	}
	if req.URL.Path != "/" {
		t.Errorf("URL.Path = %q, want /", req.URL.Path)
	}
	if got := req.Header.Get("X-Forwarded-Host"); got != "" {
		t.Errorf("X-Forwarded-Host = %q, want it stripped (the $spx_key map blanks the key when present)", got)
	}
}
