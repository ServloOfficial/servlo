package backup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// seed lays down archives for a site at the given times, so a retention run has
// something shaped like a real history to thin out.
func seed(t *testing.T, dir, site string, times []time.Time) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, at := range times {
		p := filepath.Join(dir, site+"-"+at.UTC().Format("20060102-150405")+Extension)
		if err := os.WriteFile(p, []byte("archive"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func remaining(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// A policy that keeps nothing of a period must not be read as keeping
// everything. Zero is a real answer and it is the one that frees disk.
func TestPrune_KeepsTheNewestOfEachPeriodAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	var times []time.Time
	for d := 0; d < 120; d++ {
		times = append(times, now.AddDate(0, 0, -d))
	}
	seed(t, dir, "acme", times)

	removed, err := Prune(dir, "acme", Policy{Daily: 7, Weekly: 4, Monthly: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	left := remaining(t, dir)
	// Seven days, four weeks and three months, with overlap collapsing: the
	// exact count matters less than that it is small and that today survived.
	if len(left) > 14 {
		t.Errorf("kept %d archives, which is not a thinned history: %v", len(left), left)
	}
	if len(left) < 7 {
		t.Errorf("kept only %d archives, fewer than the seven dailies asked for: %v", len(left), left)
	}
	if removed != 120-len(left) {
		t.Errorf("reported %d removed but %d are gone", removed, 120-len(left))
	}
	newest := "acme-" + now.Format("20060102-150405") + Extension
	if !slices.Contains(left, newest) {
		t.Errorf("the most recent backup was deleted: %v", left)
	}
}

// Another site's archives are none of this site's business. A sweep that took
// them would delete backups nobody asked it to touch.
func TestPrune_LeavesOtherSitesAlone(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	var times []time.Time
	for d := 0; d < 30; d++ {
		times = append(times, now.AddDate(0, 0, -d))
	}
	seed(t, dir, "acme", times)
	seed(t, dir, "other", times)

	if _, err := Prune(dir, "acme", Policy{Daily: 2}, now); err != nil {
		t.Fatal(err)
	}
	var others int
	for _, name := range remaining(t, dir) {
		if len(name) > 6 && name[:6] == "other-" {
			others++
		}
	}
	if others != 30 {
		t.Errorf("the sweep took %d of another site's archives", 30-others)
	}
}

// A site whose name is a prefix of another's is the trap: acme must not sweep
// acme-two's archives, the same way the cron unit scan has to tell them apart.
func TestPrune_DoesNotTakeASiteWhoseNameSharesAPrefix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	var times []time.Time
	for d := 0; d < 10; d++ {
		times = append(times, now.AddDate(0, 0, -d))
	}
	seed(t, dir, "acme", times)
	seed(t, dir, "acme_two", times)

	if _, err := Prune(dir, "acme", Policy{Daily: 1}, now); err != nil {
		t.Fatal(err)
	}
	var twos int
	for _, name := range remaining(t, dir) {
		if len(name) > 9 && name[:9] == "acme_two-" {
			twos++
		}
	}
	if twos != 10 {
		t.Errorf("sweeping acme took %d of acme_two's archives", 10-twos)
	}
}

// A partial file from an interrupted backup is not an archive and must not be
// counted as one, or a run that failed would push a good archive out of the
// window it was holding.
func TestPrune_IgnoresAnythingThatIsNotAnArchive(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	seed(t, dir, "acme", []time.Time{now, now.AddDate(0, 0, -1)})
	// A partial from an interrupted run, a checksum sidecar and a note someone
	// left. None is an archive, and the two that carry the site's name are the
	// ones a sweep matching on the prefix alone would take.
	// The last one is the trap: named exactly like an archive but without the
	// extension, which is what an operator decompressing one by hand leaves
	// behind. Only the extension check tells it apart from the real thing.
	bystanders := []string{".partial-123", "acme-20260809-030000" + Extension + ".sha256", "acme-notes.txt", "acme-20260809-020000"}
	for _, name := range bystanders {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not an archive"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Prune(dir, "acme", Policy{Daily: 2}, now); err != nil {
		t.Fatal(err)
	}
	left := remaining(t, dir)
	for _, name := range bystanders {
		if !slices.Contains(left, name) {
			t.Errorf("the sweep deleted %s, which is not an archive", name)
		}
	}
	if len(left) != 2+len(bystanders) {
		t.Errorf("expected both archives and every bystander to be left, got %v", left)
	}
}

// An empty policy keeps everything rather than deleting everything. Getting
// this backwards would wipe a server's backups the first time a config was
// missing a field.
func TestPrune_AnEmptyPolicyKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	seed(t, dir, "acme", []time.Time{now, now.AddDate(0, 0, -1), now.AddDate(0, 0, -400)})

	removed, err := Prune(dir, "acme", Policy{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 || len(remaining(t, dir)) != 3 {
		t.Errorf("an unset policy deleted %d archives", removed)
	}
}
