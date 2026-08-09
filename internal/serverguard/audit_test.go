package serverguard

import (
	"os"
	"path/filepath"
	"testing"
)

func findingTitled(t *testing.T, findings []Finding, want string) Finding {
	t.Helper()
	for _, f := range findings {
		if f.Title == want {
			return f
		}
	}
	t.Fatalf("no finding titled %q in %v", want, titles(findings))
	return Finding{}
}

func titles(findings []Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Title
	}
	return out
}

// A credential another account on the box can read is the finding, and it is
// bad rather than a note. Servlo runs every site as one Linux user, so "other
// accounts" is a real boundary and this is one of the few places it holds.
func TestAudit_FlagsACredentialAnyoneCanRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := filepath.Join(dir, "servlo")
	if err := os.MkdirAll(cfg, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "databases.yaml"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	f := findingTitled(t, configModeFindings(), "A stored credential is readable by other accounts on this server")
	if f.Severity != Bad {
		t.Errorf("severity = %q, want %q", f.Severity, Bad)
	}
	if len(f.Fix) == 0 {
		t.Error("the finding does not say how to fix it")
	}
}

// A private one passes, and the audit says what it looked at rather than only
// what it disliked. An audit that reports nothing is indistinguishable from one
// that did not run.
func TestAudit_SaysWhatItChecked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := filepath.Join(dir, "servlo")
	if err := os.MkdirAll(cfg, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "databases.yaml"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	findings := configModeFindings()
	if len(findings) != 1 || findings[0].Severity != OK {
		t.Errorf("a server with private credentials got %v", findings)
	}
}

// Nothing configured yet is not a pass and not a failure: there is nothing to
// check, and inventing a green tick for it would be a lie.
func TestAudit_SaysNothingWhenThereIsNothingToCheck(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if findings := configModeFindings(); len(findings) != 0 {
		t.Errorf("a server with no stored credentials reported %v", titles(findings))
	}
}

// Every fix is something a person runs. Servlo never runs these, and a fix that
// looked runnable would invite somebody to wire it up.
func TestAudit_EveryFixIsForAPersonToRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, f := range Audit(DefaultWant(22)) {
		for _, cmd := range f.Fix {
			if !hasPrefix(cmd, "sudo ") && !hasPrefix(cmd, "chmod ") {
				t.Errorf("%q is not written as something to hand to a person", cmd)
			}
		}
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
