package deploy

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	gitpkg "github.com/ServloOfficial/servlo/internal/git"
)

// A real repository, because the whole question this story answers is what git
// actually does to these directories, and a stub would only assert what I
// assumed it does.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wordpressClone builds a repository that tracks wp-content, a developer's
// checkout of it, and the deployed site. It returns the last two: the developer
// checkout is where a change is made and pushed, and the site is where a deploy
// runs.
//
// Tracking those directories is the case that matters. On an install where they
// are gitignored a pull already cannot touch them, so the exclude list would
// have nothing to do; the sites that lose a client's plugins are the ones whose
// repository committed them.
func wordpressClone(t *testing.T) (dev, site string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dev = filepath.Join(root, "dev")
	site = filepath.Join(root, "site")

	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q", "--bare", "-b", "main", origin)

	git(t, dev, "init", "-q", "-b", "main")
	git(t, dev, "remote", "add", "origin", origin)
	write(t, filepath.Join(dev, "index.php"), "<?php // core v1\n")
	write(t, filepath.Join(dev, "wp-content/plugins/akismet/akismet.php"), "<?php // akismet\n")
	write(t, filepath.Join(dev, "wp-content/uploads/2024/logo.png"), "\x89PNG binary\x00bytes\n")
	git(t, dev, "add", "index.php", "wp-content")
	git(t, dev, "commit", "-qm", "one")
	git(t, dev, "push", "-q", "-u", "origin", "main")

	git(t, root, "clone", "-q", origin, site)
	return dev, site
}

// push publishes the developer checkout, which is what a deploy then pulls.
func push(t *testing.T, dev string) {
	t.Helper()
	git(t, dev, "push", "-q", "origin", "main")
}

var errWriteFailed = errors.New("the disk is full")

// testOptions is a real deploy against a real repository, with only the parts
// that need a database, a container or a shell stubbed out. Everything about
// git is the real thing, which is the point of this file.
func testOptions(site string, out writer, excludes []string) Options {
	return Options{
		Site:         &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: site},
		Out:          out,
		Git:          gitpkg.Run,
		Head:         Head,
		Keep:         Keep,
		Excludes:     func(*config.Site) ([]string, error) { return excludes, nil },
		BuildWarning: func(string) string { return "" },
		Snapshot:     func(*config.Site) (string, error) { return "", errors.New("no database in this test") },
		RunScript:    func(string, string, writer) error { return nil },
		Reload:       func(*config.Site) error { return nil },
		Migrates:     func(*config.Site) (bool, error) { return false, nil },
		Script:       func(*config.Site) (string, error) { return "", nil },
	}
}

func present(t *testing.T, dir, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

// The story, end to end: a plugin the client installed is in the repository,
// the next commit removes it, and after the deploy the plugin is still there.
func TestRun_KeepsAnExcludedPluginTheUpdateRemoved(t *testing.T) {
	dev, site := wordpressClone(t)

	// The client installs a plugin through wp-admin, and the operator commits
	// it, which is how these directories come to be tracked at all.
	write(t, filepath.Join(site, "wp-content/plugins/woocommerce/woocommerce.php"), "<?php // the shop\n")
	git(t, site, "add", "wp-content/plugins/woocommerce")
	git(t, site, "commit", "-qm", "the client installed woocommerce")
	git(t, site, "push", "-q", "origin", "main")

	// A developer, working from a checkout that never had it, removes the whole
	// plugins directory and pushes.
	git(t, dev, "pull", "-q", "--ff-only", "origin", "main")
	git(t, dev, "rm", "-qr", "wp-content/plugins")
	write(t, filepath.Join(dev, "index.php"), "<?php // core v2\n")
	git(t, dev, "add", "index.php")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	var out bytes.Buffer
	res, err := Run(testOptions(site, &out, []string{"wp-content/uploads", "wp-content/plugins"}))
	if err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}

	if !present(t, site, "wp-content/plugins/woocommerce/woocommerce.php") {
		t.Error("the deploy deleted the client's installed plugin")
	}
	if !present(t, site, "wp-content/plugins/akismet/akismet.php") {
		t.Error("the deploy deleted a plugin that was there before it")
	}
	// And it did deploy: the pull happened and the core file moved on.
	if body, _ := os.ReadFile(filepath.Join(site, "index.php")); !strings.Contains(string(body), "v2") {
		t.Errorf("the pull did not happen: index.php is %q", body)
	}
	if res.FromCommit == res.ToCommit {
		t.Error("the deploy reported no movement")
	}
	if !strings.Contains(out.String(), "wp-content/plugins") {
		t.Errorf("the operator was not told which paths were kept:\n%s", out.String())
	}
}

// Restoring must not leave the tree in a state git will refuse to pull into
// next time. A protection that works once and then wedges every later deploy is
// worse than none, because it fails on the deploy after the one being watched.
func TestRun_LeavesTheTreePullableAfterKeepingAPath(t *testing.T) {
	dev, site := wordpressClone(t)
	git(t, dev, "rm", "-qr", "wp-content/plugins")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	var out bytes.Buffer
	if _, err := Run(testOptions(site, &out, []string{"wp-content/plugins"})); err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out.String())
	}

	// The kept file must be untracked now, not a staged or modified tracked
	// one: that is the difference between a clean tree and a permanent
	// conflict.
	if status := git(t, site, "status", "--porcelain", "--", "wp-content/plugins"); !strings.HasPrefix(status, "??") {
		t.Errorf("status = %q, want the kept path untracked", status)
	}

	write(t, filepath.Join(dev, "index.php"), "<?php // core v3\n")
	git(t, dev, "add", "index.php")
	git(t, dev, "commit", "-qm", "three")
	push(t, dev)

	out.Reset()
	if _, err := Run(testOptions(site, &out, []string{"wp-content/plugins"})); err != nil {
		t.Fatalf("the deploy after a kept path failed: %v\n%s", err, out.String())
	}
	if !present(t, site, "wp-content/plugins/akismet/akismet.php") {
		t.Error("the second deploy deleted what the first one kept")
	}
}

// Content, not just a path. A restored upload that is empty or line-ending
// mangled is a corrupted image, which looks like a deleted one to the client.
func TestRun_RestoresTheExactBytes(t *testing.T) {
	dev, site := wordpressClone(t)
	original, err := os.ReadFile(filepath.Join(site, "wp-content/uploads/2024/logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	git(t, dev, "rm", "-qr", "wp-content/uploads")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	var out bytes.Buffer
	if _, err := Run(testOptions(site, &out, []string{"wp-content/uploads"})); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}

	got, err := os.ReadFile(filepath.Join(site, "wp-content/uploads/2024/logo.png"))
	if err != nil {
		t.Fatalf("the upload is gone: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("the restored upload is not the original: %q vs %q", got, original)
	}
}

// The list is what decides. A path nobody excluded is the repository's to
// remove, and keeping it anyway would make a deleted file undeletable.
func TestRun_DeletesWhatIsNotExcluded(t *testing.T) {
	dev, site := wordpressClone(t)
	git(t, dev, "rm", "-qr", "wp-content/plugins", "wp-content/uploads")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	var out bytes.Buffer
	if _, err := Run(testOptions(site, &out, []string{"wp-content/uploads"})); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}

	if present(t, site, "wp-content/plugins/akismet/akismet.php") {
		t.Error("a path that was not on the list was kept anyway")
	}
	if !present(t, site, "wp-content/uploads/2024/logo.png") {
		t.Error("the path that was on the list was deleted")
	}
}

// A prefix is a path segment, not a string. "wp-content/upload" must not reach
// into "wp-content/uploads-old", or an operator's list quietly protects more
// than it names.
func TestKeptPaths_MatchesWholeSegments(t *testing.T) {
	dev, site := wordpressClone(t)
	write(t, filepath.Join(dev, "wp-content/uploads-old/x.png"), "old\n")
	git(t, dev, "add", "wp-content/uploads-old")
	git(t, dev, "commit", "-qm", "add the lookalike")
	push(t, dev)
	git(t, site, "pull", "-q", "--ff-only", "origin", "main")

	git(t, dev, "rm", "-qr", "wp-content/uploads", "wp-content/uploads-old")
	git(t, dev, "commit", "-qm", "remove both")
	push(t, dev)

	var out bytes.Buffer
	if _, err := Run(testOptions(site, &out, []string{"wp-content/uploads"})); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}

	if !present(t, site, "wp-content/uploads/2024/logo.png") {
		t.Error("the excluded directory was deleted")
	}
	if present(t, site, "wp-content/uploads-old/x.png") {
		t.Error("a directory that merely starts with an excluded path was kept")
	}
}

// A site with no list deploys exactly as it did before this existed.
func TestRun_NoExcludeListChangesNothing(t *testing.T) {
	dev, site := wordpressClone(t)
	git(t, dev, "rm", "-qr", "wp-content/plugins")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	var out bytes.Buffer
	if _, err := Run(testOptions(site, &out, nil)); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}

	if present(t, site, "wp-content/plugins/akismet/akismet.php") {
		t.Error("a site with no exclude list kept a path anyway")
	}
	if strings.Contains(out.String(), "Kept") {
		t.Errorf("a deploy that kept nothing said it kept something:\n%s", out.String())
	}
}

// Failing to keep a file is not something to carry on past. The operator asked
// for that path to survive, and a deploy that reports success having lost it is
// the failure this whole story exists to prevent.
func TestRun_StopsWhenItCannotKeepAPath(t *testing.T) {
	dev, site := wordpressClone(t)
	git(t, dev, "rm", "-qr", "wp-content/plugins")
	git(t, dev, "commit", "-qm", "two")
	push(t, dev)

	o := testOptions(site, &bytes.Buffer{}, []string{"wp-content/plugins"})
	o.Keep = func(string, string, string, []string, writer) ([]string, error) {
		return nil, errWriteFailed
	}
	if _, err := Run(o); err == nil {
		t.Fatal("the deploy reported success after failing to keep an excluded path")
	}
}

// KeptPaths asks git rather than walking the tree, so it only names files the
// update actually removed.
func TestKeptPaths_NamesOnlyWhatTheUpdateRemoved(t *testing.T) {
	dev, site := wordpressClone(t)
	write(t, filepath.Join(dev, "wp-content/plugins/akismet/extra.php"), "<?php // added\n")
	git(t, dev, "rm", "-q", "wp-content/plugins/akismet/akismet.php")
	git(t, dev, "add", "wp-content/plugins")
	git(t, dev, "commit", "-qm", "swap a file")
	push(t, dev)

	from := git(t, site, "rev-parse", "HEAD")
	git(t, site, "pull", "-q", "--ff-only", "origin", "main")
	to := git(t, site, "rev-parse", "HEAD")

	got, err := deletedUnder(site, from, to, []string{"wp-content/plugins", "wp-content/uploads"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"wp-content/plugins/akismet/akismet.php"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("deletedUnder = %v, want %v", got, want)
	}
}
