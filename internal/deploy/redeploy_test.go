package deploy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// deployTwice moves the site forward one commit through a real deploy, so the
// history has something to go back from. It returns the commit the site was on
// before that deploy.
func deployTwice(t *testing.T, dev, site string) string {
	t.Helper()
	before := git(t, site, "rev-parse", "HEAD")

	write(t, filepath.Join(dev, "index.php"), "<?php // core v2\n")
	write(t, filepath.Join(dev, "broken.php"), "<?php // the bad change\n")
	git(t, dev, "add", "index.php", "broken.php")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = historySite(t, site)
	res, err := Run(o)
	if err != nil {
		t.Fatalf("the deploy that redeploy undoes failed: %v\n%s", err, out.String())
	}
	if err := Record(o.Site, Entry{From: res.FromCommit, To: res.ToCommit, OK: true}); err != nil {
		t.Fatal(err)
	}
	return before
}

// historySite is a site whose history lands in this test's own data directory.
func historySite(t *testing.T, path string) *config.Site {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: path}
}

// The point of the story: one click puts the site back on the commit it was
// running before the last deploy.
func TestRedeploy_PutsTheSiteBackOnThePreviousCommit(t *testing.T) {
	dev, site := wordpressClone(t)
	before := deployTwice(t, dev, site)

	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}
	res, err := Redeploy(o, before)
	if err != nil {
		t.Fatalf("redeploy: %v\n%s", err, out.String())
	}

	if head := git(t, site, "rev-parse", "HEAD"); head != before {
		t.Errorf("HEAD = %s, want the previous commit %s", short(head), short(before))
	}
	if body, _ := os.ReadFile(filepath.Join(site, "index.php")); !strings.Contains(string(body), "v1") {
		t.Errorf("the working tree is still on the new code: %q", body)
	}
	if present(t, site, "broken.php") {
		t.Error("the bad change is still in the tree")
	}
	if res.ToCommit != before {
		t.Errorf("ToCommit = %s, want %s", short(res.ToCommit), short(before))
	}
	if !res.Redeploy {
		t.Error("the result does not mark itself as a redeploy")
	}
}

// A redeploy is a deploy: the script has to run again, because the previous
// commit's dependencies and caches are not the ones currently built.
func TestRedeploy_RunsTheScriptAgain(t *testing.T) {
	dev, site := wordpressClone(t)
	before := deployTwice(t, dev, site)

	ran := 0
	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}
	o.Script = func(*config.Site) (string, error) { return "echo rebuilding\n", nil }
	o.RunScript = func(string, string, writer) error { ran++; return nil }

	if _, err := Redeploy(o, before); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	if ran != 1 {
		t.Errorf("the script ran %d times, want once", ran)
	}
}

// It never pulls. A redeploy that fetched would race the thing it is undoing:
// the operator is going backwards, and going to the network first is how you
// end up back on the commit you were escaping.
func TestRedeploy_DoesNotPull(t *testing.T) {
	dev, site := wordpressClone(t)
	before := deployTwice(t, dev, site)

	var pulled bool
	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}
	realGit := o.Git
	o.Git = func(dir string, w writer, args ...string) error {
		if len(args) > 0 && args[0] == "pull" {
			pulled = true
		}
		return realGit(dir, w, args...)
	}

	if _, err := Redeploy(o, before); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	if pulled {
		t.Error("a redeploy pulled")
	}
}

// The exclude list applies here too, and this is the case that needs it most: a
// checkout removes tracked files the target commit does not have, which is
// exactly how a plugin installed after that commit disappears.
func TestRedeploy_KeepsExcludedPathsTheCheckoutWouldRemove(t *testing.T) {
	dev, site := wordpressClone(t)
	before := git(t, site, "rev-parse", "HEAD")

	// A plugin the client installed after the commit being gone back to.
	write(t, filepath.Join(dev, "wp-content/plugins/woocommerce/woocommerce.php"), "<?php // the shop\n")
	git(t, dev, "add", "wp-content/plugins/woocommerce")
	git(t, dev, "commit", "-qm", "the client installed woocommerce")
	push(t, dev)

	var out bytes.Buffer
	o := testOptions(site, &out, []string{"wp-content/plugins"})
	o.Site = historySite(t, site)
	if _, err := Run(o); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}

	out.Reset()
	o2 := testOptions(site, &out, []string{"wp-content/plugins"})
	o2.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}
	if _, err := Redeploy(o2, before); err != nil {
		t.Fatalf("redeploy: %v\n%s", err, out.String())
	}

	if !present(t, site, "wp-content/plugins/woocommerce/woocommerce.php") {
		t.Error("going back a commit deleted the client's plugin")
	}
}

// A commit that is not in the repository is refused before anything moves,
// rather than leaving the tree half-checked-out.
func TestRedeploy_RefusesACommitTheRepositoryDoesNotHave(t *testing.T) {
	dev, site := wordpressClone(t)
	deployTwice(t, dev, site)
	head := git(t, site, "rev-parse", "HEAD")

	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}

	if _, err := Redeploy(o, "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Fatal("a commit the repository does not have was accepted")
	}
	if now := git(t, site, "rev-parse", "HEAD"); now != head {
		t.Errorf("the tree moved anyway: HEAD = %s", short(now))
	}
}

// A failed script does not reload PHP, the same as a forward deploy: OPcache
// keeps serving what it has, so visitors stay on the version that worked rather
// than on a half-prepared rollback.
func TestRedeploy_DoesNotReloadWhenTheScriptFails(t *testing.T) {
	dev, site := wordpressClone(t)
	before := deployTwice(t, dev, site)

	reloaded := false
	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}
	o.Script = func(*config.Site) (string, error) { return "exit 1\n", nil }
	o.RunScript = func(string, string, writer) error { return errWriteFailed }
	o.Reload = func(*config.Site) error { reloaded = true; return nil }

	if _, err := Redeploy(o, before); err == nil {
		t.Fatal("a redeploy whose script failed reported success")
	}
	if reloaded {
		t.Error("PHP was reloaded after the script failed")
	}
}

// No database backup and no migration revert. A redeploy that tried to undo a
// migration would be guessing at the operator's schema, and one that silently
// did nothing about it would be worse.
func TestRedeploy_TakesNoSnapshotAndRevertsNoMigration(t *testing.T) {
	dev, site := wordpressClone(t)
	before := deployTwice(t, dev, site)

	snapshots := 0
	var out bytes.Buffer
	o := testOptions(site, &out, nil)
	o.Site = &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site}
	o.Migrates = func(*config.Site) (bool, error) { return true, nil }
	o.Snapshot = func(*config.Site) (string, error) { snapshots++; return "snap", nil }

	if _, err := Redeploy(o, before); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	if snapshots != 0 {
		t.Error("a redeploy took a database backup, which implies it can undo one")
	}
	// And it says so in as many words, because the operator is about to assume
	// otherwise and act on it. Mentioning migrations is not enough: the output
	// has to state that they are not undone.
	said := strings.ToLower(out.String())
	if !strings.Contains(said, "migrations are not undone") {
		t.Errorf("the output never says migrations are not undone:\n%s", out.String())
	}
	if !strings.Contains(said, "snapshot") {
		t.Errorf("the output does not point at the snapshot as the way to put data back:\n%s", out.String())
	}
}

// The history says which change went out, which means reading the commit, not
// just its hash.
func TestCommitMeta_ReadsTheAuthorAndSubject(t *testing.T) {
	dev, site := wordpressClone(t)
	head := git(t, site, "rev-parse", "HEAD")

	author, subject := CommitMeta(site, head)

	if author != "t" {
		t.Errorf("author = %q, want the commit author", author)
	}
	if subject != "one" {
		t.Errorf("subject = %q, want the commit subject", subject)
	}
	_ = dev
}

// A commit that is not there, or a path that is not a repository, is blank
// rather than an error: the history entry is still worth writing without it.
func TestCommitMeta_BlankWhenItCannotRead(t *testing.T) {
	author, subject := CommitMeta(t.TempDir(), "deadbeef")

	if author != "" || subject != "" {
		t.Errorf("got %q / %q, want both blank", author, subject)
	}
}
