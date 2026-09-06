package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// writeProjectYAML writes a .servlo.yaml containing a commands: block so tests
// can exercise the merge + run paths without depending on the built-in
// laravel framework def.
func writeProjectYAML(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".servlo.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func registerSite(t *testing.T, name, domain string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	sitePath := t.TempDir()
	if err := config.AddSite(config.Site{Name: name, Path: sitePath, Domains: []string{domain}}); err != nil {
		t.Fatal(err)
	}
	return sitePath
}

func TestCommandsList_ReturnsProjectCommands(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: hello
    label: Say hello
    command: echo hi
    output: text
  - name: deploy
    label: Deploy
    command: ./bin/deploy
    confirm: true
    output: silent
`)
	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/commands", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Commands []config.FrameworkCommand `json:"commands"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Commands) != 2 {
		t.Fatalf("want 2 commands, got %d: %+v", len(resp.Commands), resp.Commands)
	}
	if resp.Commands[0].Name != "hello" || resp.Commands[1].Name != "deploy" {
		t.Errorf("order: %+v", resp.Commands)
	}
	if !resp.Commands[1].Confirm {
		t.Errorf("deploy.Confirm should be true: %+v", resp.Commands[1])
	}
}

func TestCommandsRun_StreamsStdoutThenDone(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: echo
    label: Echo
    command: printf 'first\nsecond\n'
    output: text
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/echo/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "event: stdout\ndata: first\n") {
		t.Errorf("expected stdout event for 'first': %q", body)
	}
	if !strings.Contains(body, "event: stdout\ndata: second\n") {
		t.Errorf("expected stdout event for 'second': %q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("expected done event: %q", body)
	}
	// Parse the done event payload.
	idx := strings.Index(body, "event: done\ndata: ")
	if idx < 0 {
		t.Fatalf("done event not found")
	}
	rest := body[idx+len("event: done\ndata: "):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		t.Fatalf("done payload not terminated: %q", rest)
	}
	var done struct {
		Exit       int    `json:"exit"`
		DurationMs int64  `json:"durationMs"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal([]byte(rest[:end]), &done); err != nil {
		t.Fatalf("decode done: %v (%q)", err, rest[:end])
	}
	if done.Exit != 0 {
		t.Errorf("exit code: got %d want 0", done.Exit)
	}
}

func TestCommandsRun_NonZeroExit(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: fail
    label: Fail
    command: sh -c 'echo boom; exit 7'
    output: text
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/fail/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `"exit":7`) {
		t.Errorf("expected exit 7 in done payload: %q", body)
	}
}

func TestCommandsRun_URLExtraction(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: uli
    label: Login link
    command: echo 'https://acme.test/login/one-time/abc123'
    output: url
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/uli/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `"url":"https://acme.test/login/one-time/abc123"`) {
		t.Errorf("expected url in done payload: %q", body)
	}
}

func TestCommandsRun_NotFound(t *testing.T) {
	registerSite(t, "acme", "acme.test")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/missing/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "command not found") {
		t.Errorf("expected not-found error: %q", rec.Body.String())
	}
}

// A command declaring the deleted terminal output must not run at all. The
// store no longer produces one, but a hand-edited project file still can, and
// spawning a desktop emulator on a headless droplet is not a thing to attempt.
func TestCommandsRun_RejectsTheDeletedTerminalOutput(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: shell
    label: Open shell
    command: bash
    output: terminal
`)

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/shell/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "event: stdout") || strings.Contains(body, "event: done") {
		t.Errorf("an invalid output ran the command anyway: %q", body)
	}
	if !strings.Contains(body, `"error"`) {
		t.Errorf("expected an error, got: %q", body)
	}
}

// A site's declared commands are part of driving that site, and the site
// permission is what says who may drive it. The browser running them is on
// somebody's laptop, which is the ordinary case rather than the exception.
func TestCommandsRun_RunsForARemoteCaller(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: hello
    label: Hi
    command: echo hi
    output: text
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/hello/run?approve=1", nil)
	req.RemoteAddr = "203.0.113.10:42000"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Errorf("the command did not run for a remote caller: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCommandsList_AllowsNonLoopback(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: hello
    label: Hi
    command: echo hi
`)
	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/commands", nil)
	req.RemoteAddr = "192.168.1.50:42000"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("list endpoint must allow read-only requests: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCommandsRun_RejectsConcurrent(t *testing.T) {
	// Pre-acquire the lock to simulate an in-flight run, then assert the
	// second request gets a 409 Conflict.
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: hello
    label: Hi
    command: sleep 5
`)
	release, _, ok := tryAcquireRun("acme", "first")
	if !ok {
		t.Fatal("could not acquire lock for setup")
	}
	defer release()

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/hello/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("concurrent run should get 409, got %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already running") {
		t.Errorf("expected 'already running' in body: %q", rec.Body.String())
	}
}

func TestCommandsRun_ConcurrentStdoutStderrNoRace(t *testing.T) {
	// Race-detector smoke test. The handler spawns two goroutines that both
	// write to the same strings.Builder and the same http.ResponseWriter
	// (one for stdout, one for stderr). Without serialization that's a
	// data race; run this test with -race to catch it.
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: noisy
    label: Noisy
    command: "for i in $(seq 1 50); do echo out-$i; echo err-$i >&2; done"
    output: text
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/noisy/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Errorf("expected done event: %q", rec.Body.String()[:200])
	}
}

// Asserts the runner prepends BinDir to PATH so php/composer/npm shims
// resolve under the daemon's restricted PATH.
func TestCommandsRun_BinDirOnPath(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	binDir := config.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(binDir, "servloshim")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho shimok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Strip BinDir from any inherited PATH so only the fix puts it back.
	t.Setenv("PATH", "/usr/bin:/bin")

	writeProjectYAML(t, sitePath, `
commands:
  - name: hi
    label: Hi
    command: servloshim
    output: text
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/hi/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "shimok") {
		t.Errorf("BinDir shim should be on PATH; got: %q", body)
	}
}

func TestCommandsRun_DisabledOverride(t *testing.T) {
	// A project entry with Disabled: true on a framework-provided name
	// must suppress the framework default, even when the framework is the
	// real laravel one. We do that here by giving the site no framework
	// (so resolveSiteCommands sees only the project entries) and asking
	// for a disabled name, which should 404.
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: cache-clear
    disabled: true
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/cache-clear/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "command not found") {
		t.Errorf("disabled command should be missing from list: %q", rec.Body.String())
	}
}

// ── Project-command consent (issue #692) ──────────────────────────────────────

func TestCommandsRun_ProjectCommandNeedsConfirm(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: optimize
    label: Optimize
    command: echo pwned
    output: text
`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/optimize/run", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "event: stdout") || strings.Contains(body, "event: done") {
		t.Errorf("a project command must not run on the host without consent: %q", body)
	}
	if !strings.Contains(body, "needsConfirm") {
		t.Errorf("expected needsConfirm response: %q", body)
	}
}

func TestCommandsRun_ApprovePersistsConsent(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: deploy
    label: Deploy
    command: echo ok
    output: text
`)
	// First run with the confirm flag runs and remembers the approval.
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/deploy/run?approve=1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("approved run should stream output: %q", rec.Body.String())
	}
	if site, _ := config.FindSite("acme"); site == nil || !site.CommandApproved("echo ok") {
		t.Fatalf("consent should be persisted to the site registry, got %+v", site)
	}
	// Second run WITHOUT the flag now runs because the approval is remembered.
	req2 := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/commands/deploy/run", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	handleSiteAction(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "event: done") {
		t.Errorf("a previously-approved command should run without re-confirming: %q", rec2.Body.String())
	}
}

func TestCommandsList_ForcesConfirmForUnapprovedProjectCommand(t *testing.T) {
	sitePath := registerSite(t, "acme", "acme.test")
	writeProjectYAML(t, sitePath, `
commands:
  - name: deploy
    label: Deploy
    command: ./bin/deploy
    output: silent
`)
	first := func() config.FrameworkCommand {
		req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/commands", nil)
		rec := httptest.NewRecorder()
		handleSiteAction(rec, req)
		var resp struct {
			Commands []config.FrameworkCommand `json:"commands"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Commands) == 0 {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
		return resp.Commands[0]
	}
	if !first().Confirm {
		t.Error("an unapproved project command must be marked Confirm so the modal shows the command")
	}
	if err := config.ApproveSiteCommand("acme", "./bin/deploy"); err != nil {
		t.Fatal(err)
	}
	if first().Confirm {
		t.Error("an approved project command should not force the confirm modal")
	}
}
