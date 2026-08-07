package cli

import (
	"testing"

	"github.com/realrashid/servlo/internal/auditlog"
)

// A shell action is attributed to whoever is at the keyboard. An entry with no
// actor reads as servlo acting on its own, which is what a renewal is and what
// a person running a command is not.
func TestRecordAudit_AttributesToTheShellUser(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	recordAudit(auditlog.Entry{Action: "test.action", Subject: "example.com"})

	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(entries))
	}
	if entries[0].Actor == "" {
		t.Error("a command-line action was recorded with no actor")
	}
	// No IP: the request came from a terminal on this machine, and a loopback
	// address would suggest the entry knows something it does not.
	if entries[0].IP != "" {
		t.Errorf("ip = %q, want empty for a shell action", entries[0].IP)
	}
}

// An actor the caller set is kept, so a future path that knows better than the
// Unix user can say so.
func TestRecordAudit_KeepsAnExplicitActor(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	recordAudit(auditlog.Entry{Action: "test.action", Actor: "alice"})

	entries, _ := auditlog.Recent(10)
	if len(entries) != 1 || entries[0].Actor != "alice" {
		t.Fatalf("entries = %+v, want the explicit actor kept", entries)
	}
}

// The environment is a fallback, not the answer: a container with no passwd
// entry still records something rather than nothing.
func TestShellActor_FallsBackToTheEnvironment(t *testing.T) {
	if shellActor() == "" {
		t.Error("shellActor found no name at all")
	}
}
