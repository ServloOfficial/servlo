package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/alerts"
	"github.com/ServloOfficial/servlo/internal/config"
)

// withDataHome points the archive directory at a fresh tmpdir, so a test starts
// from a server that has never backed anything up.
func withDataHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	return config.SiteBackupsDir()
}

func site(name string, backup *config.SiteBackup) config.Site {
	return config.Site{Name: name, Path: "/home/op/sites/" + name, Backup: backup}
}

// The one this exists for. The alert list already says a backup failed and that
// one could not be restored, and said nothing at all about a site that has
// never had one, which is the worse state and the quieter one.
func TestReportUnbacked_RaisesForASiteNothingHasEverBackedUp(t *testing.T) {
	withDataHome(t)
	raised, _ := captureAlerts(t)

	ReportUnbacked([]config.Site{site("acme", nil)}, nil)

	if len(*raised) != 1 || (*raised)[0].Kind != alerts.KindBackupNone {
		t.Fatalf("raised %+v, want one alert about a site with no backups", *raised)
	}
	if (*raised)[0].Site != "acme" {
		t.Errorf("the alert is against %q", (*raised)[0].Site)
	}
	// It has to say what to do, because an operator reading it at breakfast
	// needs the next step rather than the news.
	if !strings.Contains((*raised)[0].Message, "schedule") {
		t.Errorf("the alert does not say how to fix it:\n%s", (*raised)[0].Message)
	}
}

// An archive is something to restore from, whatever the schedule says. A site
// backed up by hand has made a choice, and an alert about it is the list crying
// wolf.
func TestReportUnbacked_SaysNothingWhenAnArchiveExists(t *testing.T) {
	dir := withDataHome(t)
	seed(t, dir, config.SiteSlug("acme"), []time.Time{time.Now().Add(-48 * time.Hour)})
	raised, cleared := captureAlerts(t)

	ReportUnbacked([]config.Site{site("acme", nil)}, nil)

	if len(*raised) != 0 {
		t.Fatalf("raised %+v for a site that has an archive", *raised)
	}
	if len(*cleared) != 1 || (*cleared)[0] != alerts.KindBackupNone+"/acme" {
		t.Errorf("cleared %v, want the alert taken off the list", *cleared)
	}
}

// A schedule means one is coming tonight. Alerting in the window between
// scheduling a site and its first run would fire on exactly the operator who
// did the right thing an hour ago.
func TestReportUnbacked_SaysNothingWhenAScheduleWillTakeOne(t *testing.T) {
	withDataHome(t)
	raised, _ := captureAlerts(t)

	ReportUnbacked([]config.Site{site("acme", &config.SiteBackup{Schedule: "*-*-* 03:30:00"})}, nil)

	if len(*raised) != 0 {
		t.Fatalf("raised %+v for a site whose schedule has not fired yet", *raised)
	}
}

// A schedule switched off is a schedule that will not run, so a site holding
// one and nothing else is as unbacked as a site holding neither.
func TestReportUnbacked_ADisabledScheduleWillNotTakeOne(t *testing.T) {
	withDataHome(t)
	raised, _ := captureAlerts(t)

	ReportUnbacked([]config.Site{site("acme", &config.SiteBackup{Schedule: "*-*-* 03:30:00", Disabled: true})}, nil)

	if len(*raised) != 1 || (*raised)[0].Kind != alerts.KindBackupNone {
		t.Fatalf("raised %+v, want an alert for a site whose schedule is switched off", *raised)
	}
}

// A parked domain is a placeholder with nothing in it. Alerting that it has no
// backups is the clutter that teaches an operator to stop reading the list.
func TestReportUnbacked_LeavesAParkedDomainAlone(t *testing.T) {
	withDataHome(t)
	raised, cleared := captureAlerts(t)

	parked := config.Site{Name: "held", Path: "/home/op/Servlo/held"}
	ReportUnbacked([]config.Site{parked}, []string{"/home/op/Servlo"})

	if len(*raised) != 0 {
		t.Fatalf("raised %+v for a parked domain", *raised)
	}
	if len(*cleared) != 0 {
		t.Errorf("cleared %v for a parked domain, which was never on the list", *cleared)
	}
}

// Sites are reported one at a time, so the one with no backups is named rather
// than the server being told in general that something is wrong somewhere.
func TestReportUnbacked_NamesTheSiteAndLeavesTheOthers(t *testing.T) {
	dir := withDataHome(t)
	seed(t, dir, config.SiteSlug("safe"), []time.Time{time.Now()})
	raised, _ := captureAlerts(t)

	ReportUnbacked([]config.Site{site("safe", nil), site("exposed", nil)}, nil)

	if len(*raised) != 1 {
		t.Fatalf("raised %+v, want only the site with nothing", *raised)
	}
	if (*raised)[0].Site != "exposed" {
		t.Errorf("the alert names %q", (*raised)[0].Site)
	}
}

// The names come back as well as going to the panel, so an operator running the
// command by hand is told on the spot instead of being sent to look.
func TestReportUnbacked_ReturnsWhatItRaisedFor(t *testing.T) {
	dir := withDataHome(t)
	seed(t, dir, config.SiteSlug("safe"), []time.Time{time.Now()})
	captureAlerts(t)

	got := ReportUnbacked([]config.Site{site("safe", nil), site("exposed", nil)}, nil)

	if len(got) != 1 || got[0] != "exposed" {
		t.Errorf("returned %v, want the one site with nothing", got)
	}
}

// The archive directory not being readable is not the same as a site having no
// backups, and an alert that cannot tell the two apart is worse than none.
func TestReportUnbacked_StaysQuietWhenItCannotTell(t *testing.T) {
	dir := withDataHome(t)
	// A regular file where the archive directory should be, so reading it fails
	// with something other than "there is nothing here yet".
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	raised, _ := captureAlerts(t)

	ReportUnbacked([]config.Site{site("acme", nil)}, nil)

	if len(*raised) != 0 {
		t.Fatalf("raised %+v when it could not read the archives at all", *raised)
	}
}
