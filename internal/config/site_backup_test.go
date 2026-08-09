package config

import (
	"slices"
	"testing"
)

// Every field on Site has to be listed in siteYAML and in both conversions, and
// a field that is not is dropped on the next save with nothing to say so. That
// is what happened to the backup fields the first time: the schedule was
// accepted, reported as set, and gone by the time anything read it back.
func TestSiteRoundTrip_KeepsTheBackupFields(t *testing.T) {
	exclude := []string{"vendor", "node_modules"}
	in := Site{
		Name:          "acme",
		Path:          "/home/dev/code/acme",
		BackupExclude: &exclude,
		Backup: &SiteBackup{
			Schedule: "*-*-* 03:30:00",
			Keep:     &BackupKeep{Daily: 7, Weekly: 4, Monthly: 3},
		},
	}

	out := in.toYAML().toSite()

	if out.BackupExclude == nil {
		t.Fatal("the backup exclude list was dropped on the way through")
	}
	if !slices.Equal(*out.BackupExclude, exclude) {
		t.Errorf("exclude list = %v, want %v", *out.BackupExclude, exclude)
	}
	if out.Backup == nil {
		t.Fatal("the backup schedule was dropped on the way through")
	}
	if out.Backup.Schedule != in.Backup.Schedule {
		t.Errorf("schedule = %q, want %q", out.Backup.Schedule, in.Backup.Schedule)
	}
	if out.Backup.Keep == nil || *out.Backup.Keep != *in.Backup.Keep {
		t.Errorf("retention = %+v, want %+v", out.Backup.Keep, in.Backup.Keep)
	}
}

// An empty exclude list means this site's backup carries everything, and an
// absent one means follow the framework. Round-tripping must not turn one into
// the other, or clearing the list would silently stop working.
func TestSiteRoundTrip_TellsAnEmptyExcludeListFromAnAbsentOne(t *testing.T) {
	empty := []string{}
	withEmpty := Site{Name: "acme", BackupExclude: &empty}.toYAML().toSite()
	if withEmpty.BackupExclude == nil {
		t.Error("an empty list came back as absent, so the site went back to following its framework")
	}

	withNone := Site{Name: "acme"}.toYAML().toSite()
	if withNone.BackupExclude != nil {
		t.Error("an absent list came back as empty, so the site stopped following its framework")
	}
}
