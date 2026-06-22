package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/feedback"
)

// runEnvLive must save and restore the previous live line rather than nilling
// the global, so it is reentrant. `lerd env` in an unlinked dir links first, and
// linking runs its own setup env step (a nested runEnvLive); if the nested call
// left envLive nil the outer line's Done/Fail would deref nil and crash. Here
// the outer env run is stood in for by a sentinel live line, and the nested run
// errors out fast (unlinked dir, non-interactive) — the only thing asserted is
// that the sentinel survives.
func TestRunEnvLive_RestoresPreviousLive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	sentinel := feedback.StartLive("outer")
	envLive = sentinel
	t.Cleanup(func() { envLive = nil })

	if err := runEnvLive(nil, nil); err == nil {
		t.Fatal("expected an error running env in an unlinked, non-interactive dir")
	}
	if envLive != sentinel {
		t.Errorf("runEnvLive left envLive = %v, want the previous live line restored", envLive)
	}
}
