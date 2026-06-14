package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestDoctorRoute_unknownBranchRefused: a branch that doesn't resolve to a
// worktree must not fall back to the parent checkout, or the doctor would
// silently diagnose the main site's .env and database instead of the worktree.
func TestDoctorRoute_unknownBranchRefused(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}, Framework: "laravel"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/doctor?branch=ghost", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	if !doctorRoute(rec, req, "acme.test", []string{"doctor"}) {
		t.Fatal("doctorRoute did not handle the request")
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Errorf("unknown branch: expected an error, got %s", rec.Body.String())
	}
	if _, ok := resp["checks"]; ok {
		t.Error("unknown branch must not return checks (would be the parent's)")
	}
}
