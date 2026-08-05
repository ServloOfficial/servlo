package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// stubReloadTest swaps NginxReloadFn/NginxTestFn for the duration of a test;
// the real ones shell into the servlo-nginx container, unavailable in CI.
func stubReloadTest(t *testing.T, testOut string, testErr error) (reloads *int) {
	t.Helper()
	calls := 0
	prevReload, prevTest := NginxReloadFn, NginxTestFn
	NginxReloadFn = func() error { calls++; return nil }
	NginxTestFn = func() (string, error) { return testOut, testErr }
	t.Cleanup(func() { NginxReloadFn, NginxTestFn = prevReload, prevTest })
	return &calls
}

func writeOverride(t *testing.T, domain, content string) string {
	t.Helper()
	if err := os.MkdirAll(config.NginxCustomD(), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(config.NginxCustomD(), domain+".conf")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadCustomNginx_seedsTemplateWhenMissing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got, err := ReadCustomNginx("acme.test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Exists {
		t.Fatal("expected Exists=false for missing override")
	}
	if !strings.Contains(got.Body, "Servlo per-site nginx overrides") {
		t.Fatalf("expected seeded template, got %q", got.Body)
	}
}

func TestSaveCustomNginx_writesAndReloads(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubReloadTest(t, "test is successful", nil)
	res, err := SaveCustomNginx("acme.test", "# v1\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || *reloads != 1 {
		t.Fatalf("res=%+v reloads=%d", res, *reloads)
	}
	b, _ := os.ReadFile(CustomNginxPath("acme.test"))
	if string(b) != "# v1\n" {
		t.Fatalf("on-disk content = %q", string(b))
	}
}

func TestSaveCustomNginx_rollsBackWhenValidationNamesOurFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubReloadTest(t, "nginx: [emerg] invalid in /etc/nginx/custom.d/acme.test.conf:2", os.ErrInvalid)
	writeOverride(t, "acme.test", "# good\n")
	res, err := SaveCustomNginx("acme.test", "bogus;\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected validation failure")
	}
	b, _ := os.ReadFile(CustomNginxPath("acme.test"))
	if string(b) != "# good\n" {
		t.Fatalf("expected rollback, got %q", string(b))
	}
}

func TestResetCustomNginx_removesFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubReloadTest(t, "ok", nil)
	p := writeOverride(t, "acme.test", "# x\n")
	if err := ResetCustomNginx("acme.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("expected override removed")
	}
}

func TestListAndRestoreBackup_roundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubReloadTest(t, "ok", nil)
	writeOverride(t, "acme.test", "# v1\n")
	if _, err := SaveCustomNginx("acme.test", "# v2\n", true); err != nil {
		t.Fatal(err)
	}
	list, err := ListCustomNginxBackups("acme.test")
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 backup, got %v err=%v", list, err)
	}
	res, err := RestoreCustomNginx("acme.test", list[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Content != "# v1\n" {
		t.Fatalf("expected restored v1, got %+v", res)
	}
}
