package ui

import (
	"net/http"
	"testing"
)

func TestWSOriginAllowed(t *testing.T) {
	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"no origin (non-browser client)", "localhost:7073", "", true},
		{"allowlisted localhost", "localhost:7073", "http://localhost:7073", true},
		{"allowlisted lerd.localhost", "lerd.localhost", "http://lerd.localhost", true},
		{"same-origin custom domain", "myapp.test", "http://myapp.test", true},
		{"same-origin LAN ip", "192.168.1.10:7073", "http://192.168.1.10:7073", true},
		{"same-origin with host casing divergence", "MyApp.test", "http://myapp.test", true},
		{"cross-site hijack attempt", "localhost:7073", "http://evil.example", false},
		{"foreign origin same path", "myapp.test", "https://attacker.test", false},
		{"malformed origin", "localhost:7073", "::::not a url", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "http://"+tc.host+"/api/lsp/php", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := wsOriginAllowed(r); got != tc.want {
				t.Fatalf("wsOriginAllowed(host=%q, origin=%q) = %v, want %v", tc.host, tc.origin, got, tc.want)
			}
		})
	}
}
