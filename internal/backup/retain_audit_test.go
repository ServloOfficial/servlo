package backup

import (
	"strings"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/auditlog"
	"github.com/ServloOfficial/servlo/internal/config"
)

// CLAUDE.md section 8 names three things that delete data and says each gets a
// typed confirmation and an audit entry: site removal, database drop, backup
// pruning. The first two are operator actions with a modal in front of them.
// Pruning is not: it runs on a timer, nobody is asked, and until now nothing
// anywhere recorded that somebody's archives were deleted.
//
// The entry carries no actor on purpose. The audit Entry documents an empty
// actor as servlo itself on a timer or a watcher pass, which is exactly what
// this is, and naming whoever happened to trigger the backup would be worse
// than saying nothing.
func TestPrune_RecordsWhatItDeleted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()

	now := time.Date(2026, 9, 9, 3, 30, 0, 0, time.UTC)
	seed(t, dir, "acme", []time.Time{
		now.Add(-1 * time.Hour),
		now.Add(-2 * time.Hour),
		now.Add(-3 * time.Hour),
	})

	// One daily slot, so two of the three go.
	removed, err := Prune(dir, "acme", Policy{Daily: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed %d archives, want 2", removed)
	}

	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action != "backup.pruned" {
			continue
		}
		if e.Subject != "acme" {
			t.Errorf("the entry does not say whose archives went: %+v", e)
		}
		if !strings.Contains(e.Detail, "2") {
			t.Errorf("the entry does not say how many went: %+v", e)
		}
		if e.Actor != "" {
			t.Errorf("a timer-driven prune named an actor: %+v", e)
		}
		return
	}
	t.Errorf("deleting somebody's backups left no entry in the audit log: %+v", entries)
}

// A sweep that deletes nothing is the ordinary case, every night, for every
// site inside its policy. An entry each time would bury the ones that matter.
func TestPrune_SaysNothingWhenItDeletesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()

	now := time.Date(2026, 9, 9, 3, 30, 0, 0, time.UTC)
	seed(t, dir, "acme", []time.Time{now.Add(-1 * time.Hour)})

	if _, err := Prune(dir, "acme", Policy{Daily: 7}, now); err != nil {
		t.Fatal(err)
	}
	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action == "backup.pruned" {
			t.Errorf("a sweep that removed nothing wrote an entry: %+v", e)
		}
	}
	_ = config.DataDir()
}
