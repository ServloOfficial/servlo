package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnitForLogPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/logs/servlo-nginx", "servlo-nginx"},
		{"/api/logs/servlo-php84-fpm", "servlo-php84-fpm"},
		{"/api/watcher/logs", "servlo-watcher"},
		{"/api/queue/alpha/logs", "servlo-queue-alpha"},
		{"/api/horizon/alpha/logs", "servlo-horizon-alpha"},
		{"/api/schedule/alpha/logs", "servlo-schedule-alpha"},
		{"/api/reverb/alpha/logs", "servlo-reverb-alpha"},
		{"/api/worker/alpha/vite/logs", "servlo-vite-alpha"},
		{"/api/worker/alpha-feature/app/logs", "servlo-app-alpha-feature"},
	}
	for _, c := range cases {
		got, ok := unitForLogPath(c.path)
		if !ok || got != c.want {
			t.Errorf("unitForLogPath(%q) = %q, %v; want %q", c.path, got, ok, c.want)
		}
	}
}

func TestUnitForLogPath_Rejects(t *testing.T) {
	for _, p := range []string{
		"",
		"/api/logs/",
		"/api/logs/nginx",                 // not a servlo- unit
		"/api/logs/servlo-nginx;rm -rf /", // shell metacharacters
		"/api/logs/servlo-nginx/../etc",
		"/api/queue//logs",
		"/api/worker/alpha/logs",
		"/api/app-logs/alpha",
		"http://evil/api/logs/servlo-nginx",
	} {
		if got, ok := unitForLogPath(p); ok {
			t.Errorf("unitForLogPath(%q) = %q, want rejected", p, got)
		}
	}
}

// Every log route resolves through the same function, so an unroutable path
// must 404 rather than stream a bogus unit.
func TestHandleUnitLogStream_RejectsUnknownPath(t *testing.T) {
	rec := httptest.NewRecorder()
	handleUnitLogStream(rec, httptest.NewRequest(http.MethodGet, "/api/queue/Bad_Site/logs", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
