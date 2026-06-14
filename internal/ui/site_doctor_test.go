package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func writeEnv(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAppKey(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// Missing key → fail with a key:generate fix.
	writeEnv(t, dir, ".env", "APP_NAME=Acme\nAPP_KEY=\n")
	if c := checkAppKey(envPath); c.Status != doctorFail || c.Fix != "key:generate" {
		t.Errorf("empty APP_KEY: got status=%q fix=%q, want fail/key:generate", c.Status, c.Fix)
	}

	// Set key → ok, no fix.
	writeEnv(t, dir, ".env", "APP_KEY=base64:abcdef==\n")
	if c := checkAppKey(envPath); c.Status != doctorOK || c.Fix != "" {
		t.Errorf("set APP_KEY: got status=%q fix=%q, want ok/none", c.Status, c.Fix)
	}
}

func TestCheckEnvDrift(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// No .env.example → not applicable.
	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	if _, ok := checkEnvDrift(dir, envPath); ok {
		t.Error("expected drift check skipped when no .env.example")
	}

	// Example declares two keys the .env lacks → warn listing both.
	writeEnv(t, dir, ".env.example", "APP_KEY=\nNEW_ONE=\nNEW_TWO=\n")
	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	c, ok := checkEnvDrift(dir, envPath)
	if !ok || c.Status != doctorWarn {
		t.Fatalf("missing keys: got ok=%v status=%q, want true/warn", ok, c.Status)
	}
	if !strings.Contains(c.Detail, "NEW_ONE") || !strings.Contains(c.Detail, "NEW_TWO") {
		t.Errorf("detail should name the missing keys, got %q", c.Detail)
	}

	// All example keys present → ok.
	writeEnv(t, dir, ".env", "APP_KEY=x\nNEW_ONE=1\nNEW_TWO=2\n")
	if c, ok := checkEnvDrift(dir, envPath); !ok || c.Status != doctorOK {
		t.Errorf("aligned env: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

// TestCheckEnvDrift_classifiesRequiredVsOptional: when the project's code reads
// some keys with a default and others without, only the no-default keys (plus
// VITE_* the frontend needs) should drive the warning; keys read with a default
// or never referenced are optional and must not turn the row red.
func TestCheckEnvDrift_classifiesRequiredVsOptional(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	writeEnv(t, dir, ".env.example", "APP_KEY=\nDB_HOST=\nLOG_LEVEL=\nVITE_THING=\nUNUSED_KEY=\n")
	writeEnv(t, dir, ".env", "") // every example key is missing

	// config/ reads APP_KEY with no default (required) and the other two with
	// defaults (optional). VITE_THING and UNUSED_KEY aren't read in PHP.
	mustMkdir(t, filepath.Join(dir, "config"))
	writeEnv(t, dir, filepath.Join("config", "app.php"),
		"<?php return [\n"+
			"  'key' => env('APP_KEY'),\n"+
			"  'host' => env('DB_HOST', '127.0.0.1'),\n"+
			"  'log' => env('LOG_LEVEL', 'debug'),\n"+
			"];\n")

	c, ok := checkEnvDrift(dir, envPath)
	if !ok || c.Status != doctorWarn {
		t.Fatalf("got ok=%v status=%q, want true/warn", ok, c.Status)
	}
	// Required: APP_KEY (no default) and VITE_THING (frontend prefix).
	if !strings.Contains(c.Detail, "APP_KEY") || !strings.Contains(c.Detail, "VITE_THING") {
		t.Errorf("detail should name required keys APP_KEY and VITE_THING, got %q", c.Detail)
	}
	// Optional keys must not be listed as required.
	for _, opt := range []string{"DB_HOST", "LOG_LEVEL", "UNUSED_KEY"} {
		if strings.Contains(c.Detail, opt) {
			t.Errorf("optional key %q should not appear in the required list: %q", opt, c.Detail)
		}
	}
}

// TestCheckEnvDrift_allOptionalStaysGreen: when every missing key is read with a
// default, the check passes quietly with an informational note instead of
// warning.
func TestCheckEnvDrift_allOptionalStaysGreen(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	writeEnv(t, dir, ".env.example", "DB_HOST=\nLOG_LEVEL=\n")
	writeEnv(t, dir, ".env", "")
	mustMkdir(t, filepath.Join(dir, "config"))
	writeEnv(t, dir, filepath.Join("config", "app.php"),
		"<?php return [\n"+
			"  'host' => env('DB_HOST', '127.0.0.1'),\n"+
			"  'log' => env('LOG_LEVEL', 'debug'),\n"+
			"];\n")

	c, ok := checkEnvDrift(dir, envPath)
	if !ok || c.Status != doctorOK {
		t.Fatalf("all-optional: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

func TestCheckAppDebug(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// production + debug on → warn (the footgun).
	writeEnv(t, dir, ".env", "APP_ENV=production\nAPP_DEBUG=true\n")
	if c := checkAppDebug(envPath); c.Status != doctorWarn {
		t.Errorf("prod+debug: got %q, want warn", c.Status)
	}

	// local + debug on → ok (normal dev).
	writeEnv(t, dir, ".env", "APP_ENV=local\nAPP_DEBUG=true\n")
	if c := checkAppDebug(envPath); c.Status != doctorOK {
		t.Errorf("local+debug: got %q, want ok", c.Status)
	}

	// production + debug off → ok.
	writeEnv(t, dir, ".env", "APP_ENV=production\nAPP_DEBUG=false\n")
	if c := checkAppDebug(envPath); c.Status != doctorOK {
		t.Errorf("prod+nodebug: got %q, want ok", c.Status)
	}
}

func TestCheckStorageLink(t *testing.T) {
	// No public disk → not applicable.
	bare := t.TempDir()
	if _, ok := checkStorageLink(bare); ok {
		t.Error("expected skip when there's no storage/app/public")
	}

	// Uses public disk, public/ exists, symlink missing → warn + fix.
	missing := t.TempDir()
	mustMkdir(t, filepath.Join(missing, "storage", "app", "public"))
	mustMkdir(t, filepath.Join(missing, "public"))
	c, ok := checkStorageLink(missing)
	if !ok || c.Status != doctorWarn || c.Fix != "storage:link" {
		t.Errorf("missing link: got ok=%v status=%q fix=%q, want true/warn/storage:link", ok, c.Status, c.Fix)
	}

	// Symlink present → ok regardless of disk layout.
	linked := t.TempDir()
	mustMkdir(t, filepath.Join(linked, "public"))
	mustMkdir(t, filepath.Join(linked, "storage", "app", "public"))
	if err := os.Symlink("../storage/app/public", filepath.Join(linked, "public", "storage")); err != nil {
		t.Fatal(err)
	}
	if c, ok := checkStorageLink(linked); !ok || c.Status != doctorOK {
		t.Errorf("present link: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

func TestMigrationsPending(t *testing.T) {
	pending := "  Migration name ....................... Batch / Status\n" +
		"  2014_10_12_000000_create_users_table .. [1] Ran\n" +
		"  2024_01_01_000000_create_orders_table . Pending\n"
	if !migrationsPending(pending) {
		t.Error("expected pending=true when a row is Pending")
	}

	allRan := "  2014_10_12_000000_create_users_table .. [1] Ran\n" +
		"  2019_08_19_000000_create_failed_jobs .. [1] Ran\n"
	if migrationsPending(allRan) {
		t.Error("expected pending=false when every row Ran")
	}
}

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

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
