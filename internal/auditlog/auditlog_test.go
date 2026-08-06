package auditlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func auditEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// The log records who did what to a machine hosting other people's sites, so
// its contents are not for anyone but the operator.
func TestAppend_FileIsOwnerOnly(t *testing.T) {
	auditEnv(t)
	if err := Append(Entry{Action: "cert.renew.failed", Subject: "example.com"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit log mode = %#o, want 0600", perm)
	}
}

// Append-only is the whole point. A log an application can rewrite is a log
// that tells you what the last writer wanted you to believe.
func TestAppend_NeverRewritesWhatIsAlreadyThere(t *testing.T) {
	auditEnv(t)
	for _, subject := range []string{"first.example", "second.example", "third.example"} {
		if err := Append(Entry{Action: "cert.renew.failed", Subject: subject}); err != nil {
			t.Fatalf("Append(%s): %v", subject, err)
		}
	}
	body, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"first.example", "second.example", "third.example"} {
		if !strings.Contains(string(body), subject) {
			t.Errorf("%s is missing; an earlier entry was overwritten:\n%s", subject, body)
		}
	}
	if lines := strings.Count(strings.TrimSpace(string(body)), "\n") + 1; lines != 3 {
		t.Errorf("log has %d lines, want one per entry", lines)
	}
}

// One entry per line, parseable, with a timestamp. A log nobody can read
// mechanically is a log nobody reads.
func TestAppend_EntriesAreStructuredAndTimestamped(t *testing.T) {
	auditEnv(t)
	if err := Append(Entry{Action: "cert.renew.failed", Subject: "example.com", Detail: "dns did not point here"}); err != nil {
		t.Fatal(err)
	}
	entries, err := Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("read %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.Action != "cert.renew.failed" || got.Subject != "example.com" {
		t.Errorf("entry round-tripped as %+v", got)
	}
	if got.At.IsZero() {
		t.Error("the entry carries no timestamp")
	}
	if time.Since(got.At) > time.Minute {
		t.Errorf("timestamp %v is not from now", got.At)
	}
}

// Secrets must never reach the log. It is 0600, but it is also the file an
// operator pastes into a support thread when something goes wrong.
func TestAppend_RedactsSecretsInDetail(t *testing.T) {
	auditEnv(t)
	if err := Append(Entry{
		Action:  "dnsprovider.set",
		Subject: "cloudflare",
		Detail:  "token=cf-super-secret-value password=hunter2 api_key=abcdef",
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(Path())
	for _, secret := range []string{"cf-super-secret-value", "hunter2", "abcdef"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("the audit log leaked %q:\n%s", secret, body)
		}
	}
}

// Recent returns newest first, since that is the question being asked: what
// just went wrong.
func TestRecent_NewestFirstAndBounded(t *testing.T) {
	auditEnv(t)
	for _, s := range []string{"one", "two", "three"} {
		if err := Append(Entry{Action: "test", Subject: s}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Recent(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d entries, want the 2 asked for", len(entries))
	}
	if entries[0].Subject != "three" {
		t.Errorf("newest entry is %q, want three", entries[0].Subject)
	}
}

// A missing log is not an error. Nothing has happened yet.
func TestRecent_EmptyLogReadsAsNoEntries(t *testing.T) {
	auditEnv(t)
	entries, err := Recent(10)
	if err != nil {
		t.Fatalf("reading a log that does not exist: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("read %d entries from a log that does not exist", len(entries))
	}
}

// A corrupt line must not hide the good ones around it. A truncated write from
// a killed process should cost that line and nothing else.
func TestRecent_SkipsACorruptLine(t *testing.T) {
	auditEnv(t)
	if err := Append(Entry{Action: "test", Subject: "good"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(Path(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{not json at all\n") //nolint:errcheck
	f.Close()                           //nolint:errcheck
	if err := Append(Entry{Action: "test", Subject: "also-good"}); err != nil {
		t.Fatal(err)
	}

	entries, err := Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("read %d entries, want the two readable ones", len(entries))
	}
}

func TestPath_IsInTheDataDirectory(t *testing.T) {
	auditEnv(t)
	if !strings.HasSuffix(Path(), filepath.Join("servlo", "audit.log")) {
		t.Errorf("audit log at %q, want it under the servlo data directory", Path())
	}
}
