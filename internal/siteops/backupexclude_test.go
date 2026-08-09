package siteops

import (
	"slices"
	"testing"
)

// A site with no list of its own follows its framework's, so a definition
// updated after the site was created reaches it.
func TestBackupExcludes_FallsBackToTheFramework(t *testing.T) {
	site := wordpressSite(t)
	got, err := BackupExcludes(site)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "wp-content/cache") {
		t.Errorf("excludes = %v, want the framework's rebuildable directories", got)
	}
	// The two things that must never be left out of a WordPress backup. On most
	// installs they exist nowhere but this server.
	for _, never := range []string{"wp-content/uploads", "wp-content/plugins"} {
		if slices.Contains(got, never) {
			t.Errorf("the framework leaves %s out of the backup, and it exists nowhere else", never)
		}
	}
}

// A site that saved its own list uses it, and a site that saved an empty one
// backs up everything. Those are two different decisions and clearing has to
// survive, the same way it does for the deploy list.
func TestBackupExcludes_TheSitesOwnListWins(t *testing.T) {
	own := []string{"cache", "tmp"}
	site := wordpressSite(t)
	site.BackupExclude = &own
	got, err := BackupExcludes(site)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, own) {
		t.Errorf("excludes = %v, want the site's own %v", got, own)
	}

	empty := []string{}
	site.BackupExclude = &empty
	got, err = BackupExcludes(site)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a site that cleared its list got %v back from the framework", got)
	}
}
