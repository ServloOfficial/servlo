package alerts

import (
	"os"
	"path/filepath"
	"testing"
)

// alerts.json is the list of what is wrong with the server, and one of the
// things that can be wrong with a server is that its disk is full. That is the
// bind: the disk alert is written by the check that fires because writes are
// already failing, and os.WriteFile empties a file before it writes a byte.
//
// What is left then is a file of zero length, and load reads that as a parse
// error rather than as an empty list. So Raise records nothing and sends
// nothing, List hands the panel an error instead of the alerts, and neither
// recovers when the disk is freed, because the truncated file is still sitting
// there. The one thing that was supposed to survive a full disk is the only
// thing guaranteed not to.
//
// Replacing the file by rename costs the staging file instead. This checks the
// operator ends up with a different file rather than the old one rewritten in
// place.
func TestRaise_ReplacesTheAlertFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	sent := 0
	orig := send
	send = func(Alert) error { sent++; return nil }
	t.Cleanup(func() { send = orig })

	if err := Raise(Alert{Kind: KindSiteDown, Site: "shop", Message: "nothing answered"}); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	before, err := os.Stat(storePath())
	if err != nil {
		t.Fatal(err)
	}

	if err := Raise(Alert{Kind: KindDiskFilling, Message: "/ is 97% full"}); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	after, err := os.Stat(storePath())
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("alerts.json was rewritten in place, so the disk alert is the write that destroys the alert list")
	}

	open, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("open alerts = %d, want 2", len(open))
	}
	if got := after.Mode().Perm(); got != 0o600 {
		t.Errorf("alerts.json mode = %v, want 0600", got)
	}

	entries, err := os.ReadDir(filepath.Dir(storePath()))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}
}
