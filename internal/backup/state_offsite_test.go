package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The archive that makes a rebuild possible never left the machine it was a
// backup of.
//
// Site archives go to every configured destination as soon as they are written,
// because a backup that only exists on the machine it is a backup of is not a
// backup. The server's own state archive was written to local disk and stopped
// there: SendEverywhere had exactly one caller and it was the site path. So the
// droplet dies, the site archives are safe offsite, and the one thing that
// makes them restorable dies with the machine. That is the scenario the state
// archive exists for.
func TestRunState_CopiesTheArchiveOffTheMachine(t *testing.T) {
	configTree(t)
	key := testKey(t)
	dir := t.TempDir()

	var sent []string
	runner := StateRunner{Dir: dir, Send: func(path, name string) []error {
		sent = append(sent, name)
		return nil
	}}

	rec, err := runner.Run(key, StateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("the state archive was not sent anywhere; sent = %v", sent)
	}
	if sent[0] != filepath.Base(rec.Path) {
		t.Errorf("sent %q, want the archive that was just written (%q)", sent[0], filepath.Base(rec.Path))
	}
}

// A destination being unreachable is reported beside the archive, not instead
// of it. The archive is on this server and usable; reporting otherwise would
// have an operator re-running a backup that worked.
func TestRunState_ADestinationFailingDoesNotFailTheBackup(t *testing.T) {
	configTree(t)
	key := testKey(t)
	dir := t.TempDir()

	runner := StateRunner{Dir: dir, Send: func(string, string) []error {
		return []error{errors.New("the bucket said no")}
	}}

	rec, err := runner.Run(key, StateOptions{})
	if err != nil {
		t.Fatalf("a destination failing failed the whole backup: %v", err)
	}
	if _, statErr := os.Stat(rec.Path); statErr != nil {
		t.Errorf("the archive is not on disk after a send failure: %v", statErr)
	}
	if len(rec.SendErrors) != 1 {
		t.Errorf("SendErrors = %v, want the one failure reported", rec.SendErrors)
	}
}

// State archives were kept forever. Site archives are thinned to a policy on
// every backup, and the same reasoning applies to a file holding every
// credential servlo has: keeping all of them is how a disk fills quietly.
func TestRunState_ThinsOlderArchives(t *testing.T) {
	configTree(t)
	key := testKey(t)
	dir := t.TempDir()

	// Ten days of state archives, one a day, all older than the policy keeps.
	now := time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC)
	for i := 1; i <= 10; i++ {
		stamp := now.AddDate(0, 0, -i).Format(stampLayout)
		name := filepath.Join(dir, StateName+"-"+stamp+Extension)
		if err := os.WriteFile(name, []byte("older"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	runner := StateRunner{Dir: dir, Policy: Policy{Daily: 3}}
	rec, err := runner.Run(key, StateOptions{Taken: now})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Pruned == 0 {
		t.Fatal("no state archive was pruned, so they accumulate forever")
	}

	left, err := listArchives(dir, StateName)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 3 {
		var names []string
		for _, a := range left {
			names = append(names, filepath.Base(a.path))
		}
		t.Errorf("%d state archives left, want the 3 the policy keeps: %v", len(left), names)
	}
}

// A site's retention sweep must not reach the state archives, and the state
// sweep must not reach a site's. They share a directory, and the one that
// deletes the other is a rebuild with nothing to rebuild from.
func TestStateAndSiteRetentionLeaveEachOtherAlone(t *testing.T) {
	configTree(t)
	dir := t.TempDir()
	now := time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC)

	write := func(prefix string, daysAgo int) string {
		stamp := now.AddDate(0, 0, -daysAgo).Format(stampLayout)
		name := filepath.Join(dir, prefix+"-"+stamp+Extension)
		if err := os.WriteFile(name, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		return name
	}

	var states, sites []string
	for i := 1; i <= 6; i++ {
		states = append(states, write(StateName, i))
		sites = append(sites, write(config.SiteSlug("acme.example"), i))
	}

	// Thin the site's archives hard. The state archives must all survive.
	if _, err := Prune(dir, config.SiteSlug("acme.example"), Policy{Daily: 1}, now); err != nil {
		t.Fatal(err)
	}
	for _, p := range states {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a site's retention sweep deleted the state archive %s", filepath.Base(p))
		}
	}

	// And the other way round.
	if _, err := Prune(dir, StateName, Policy{Daily: 1}, now); err != nil {
		t.Fatal(err)
	}
	var siteLeft int
	for _, p := range sites {
		if _, err := os.Stat(p); err == nil {
			siteLeft++
		}
	}
	if siteLeft != 1 {
		t.Errorf("%d of the site's archives left after the state sweep, want the 1 its own sweep kept", siteLeft)
	}
}
