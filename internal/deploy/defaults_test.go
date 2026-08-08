package deploy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// The script stops at the first command that fails. Without that, a composer
// install that could not reach the network is followed by a migration against
// half-installed code, and the deploy reports success.
func TestRunScript_StopsAtTheFirstFailure(t *testing.T) {
	var out bytes.Buffer

	err := RunScript(t.TempDir(), "echo one\nfalse\necho three\n", &out)

	if err == nil {
		t.Fatal("a script with a failing command reported success")
	}
	if !strings.Contains(out.String(), "one") {
		t.Errorf("the output is missing what ran before the failure:\n%s", out.String())
	}
	if strings.Contains(out.String(), "three") {
		t.Errorf("the script carried on past a failing command:\n%s", out.String())
	}
}

// The site is the working directory, so a script can say `composer install`
// rather than knowing where it lives.
func TestRunScript_RunsInTheSiteDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := RunScript(dir, "ls\n", &out); err != nil {
		t.Fatalf("RunScript: %v", err)
	}

	if !strings.Contains(out.String(), "marker") {
		t.Errorf("the script did not run in the site directory:\n%s", out.String())
	}
}

// Both streams reach the operator. A build tool that writes its errors to
// stderr and its progress to stdout would otherwise show half the story, and
// the half missing is usually the one explaining the failure.
func TestRunScript_StreamsBothStreams(t *testing.T) {
	var out bytes.Buffer

	_ = RunScript(t.TempDir(), "echo to-stdout\necho to-stderr >&2\n", &out)

	for _, want := range []string{"to-stdout", "to-stderr"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output is missing %q:\n%s", want, out.String())
		}
	}
}

// A migrating deploy on a site whose database servlo cannot identify is
// refused, not quietly run without a backup. Skipping it would make the whole
// guarantee conditional on the env file being tidy.
func TestSnapshot_RefusesWhenItCannotTellWhichDatabase(t *testing.T) {
	dir := t.TempDir()
	site := &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: dir}

	// No .env at all.
	_, err := Snapshot(site)
	if err == nil {
		t.Fatal("a backup was reported for a site with no database configured")
	}
	if !strings.Contains(err.Error(), "DB_DATABASE") {
		t.Errorf("error = %q, does not say what to set", err)
	}

	// An .env naming a host but no database is the same problem.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_HOST=servlo-mysql\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(site); err == nil {
		t.Fatal("a backup was reported for a site with no database name")
	}

	// And a database with no host: servlo would not know which service to ask.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_DATABASE=shop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(site); err == nil {
		t.Fatal("a backup was reported for a site with no database host")
	}
}

// The name says what the backup is and when, so the one an operator wants after
// a bad deploy is findable in a list months later.
func TestSnapshotName_SaysWhatItIsAndWhen(t *testing.T) {
	site := &config.Site{Name: "shop", Domains: []string{"shop.example"}}

	name := snapshotName(site)

	if !strings.HasPrefix(name, "predeploy-shop-") {
		t.Errorf("name = %q, does not say what it is or which site", name)
	}
	// A timestamp, so two deploys on the same day are told apart.
	if len(name) != len("predeploy-shop-20060102-150405") {
		t.Errorf("name = %q, has no timestamp", name)
	}
	if name == snapshotName(&config.Site{Name: "other"}) {
		t.Error("two sites produced the same backup name")
	}
}

// Defaults wires every seam. A nil one is a panic in the middle of a deploy,
// after the pull, which is the worst possible time to discover it.
func TestDefaults_WiresEverySeam(t *testing.T) {
	o := Defaults(&config.Site{Name: "shop", Domains: []string{"shop.example"}}, &bytes.Buffer{})

	if o.Git == nil || o.Head == nil || o.Snapshot == nil || o.RunScript == nil ||
		o.Reload == nil || o.Migrates == nil || o.Script == nil ||
		o.Keep == nil || o.Excludes == nil {
		t.Errorf("a seam is nil: %+v", o)
	}
	if o.Out == nil {
		t.Error("nowhere for the output to go")
	}
}
