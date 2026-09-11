package deploy

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/node"
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

// The name says what the backup is and which site it came from, so the one an
// operator wants after a bad deploy is findable in a list months later. The
// time comes from the store, which stamps every snapshot it files.
func TestSnapshotName_SaysWhatItIsAndWhichSite(t *testing.T) {
	site := &config.Site{Name: "shop", Domains: []string{"shop.example"}}

	name := snapshotName(site)

	if name != "predeploy-shop" {
		t.Errorf("name = %q, does not say what it is or which site", name)
	}
	if name == snapshotName(&config.Site{Name: "other"}) {
		t.Error("two sites produced the same backup name")
	}
}

// The name a deploy reports is the name of the thing it stored. Naming the
// backup here and again in the store left the operator looking for a snapshot
// that was on disk under a longer name.
func TestSnapshot_ReportsTheNameItActuallyStored(t *testing.T) {
	site := managedSite(t)
	prev := remoteDump
	remoteDump = func(_ dbconn.Connection, _ string, w io.Writer) error {
		_, err := io.WriteString(w, "-- the dump\n")
		return err
	}
	t.Cleanup(func() { remoteDump = prev })

	name, err := Snapshot(site)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	dir := filepath.Join(config.SnapshotsDir(), "managed", "databases", "shop", name)
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the deploy reported %q and there is no such snapshot: %v", name, err)
	}
}

// Defaults wires every seam. A nil one is a panic in the middle of a deploy,
// after the pull, which is the worst possible time to discover it.
func TestDefaults_WiresEverySeam(t *testing.T) {
	o := Defaults(&config.Site{Name: "shop", Domains: []string{"shop.example"}}, &bytes.Buffer{})

	if o.Git == nil || o.Head == nil || o.Snapshot == nil || o.RunScript == nil ||
		o.Reload == nil || o.Migrates == nil || o.Script == nil ||
		o.Keep == nil || o.Excludes == nil || o.BuildWarning == nil {
		t.Errorf("a seam is nil: %+v", o)
	}
	if o.Out == nil {
		t.Error("nowhere for the output to go")
	}
}

// A site pinned to a Node version has to get that Node in its deploy, which is
// the one place its assets are actually built. Building production bundles with
// whatever Node the daemon happens to run is how a site pinned to 22 ships
// output from 18 and nobody notices until something breaks in a browser.
func TestRunScript_RunsUnderTheSitesNodeVersion(t *testing.T) {
	dir := t.TempDir()
	var got struct {
		version string
		bin     string
		args    []string
	}
	restore := stubNode(t, nodeStub{
		available: true,
		installed: []string{"22"},
		version:   "22",
		command: func(version, bin string, args []string) *exec.Cmd {
			got.version, got.bin, got.args = version, bin, args
			return exec.Command("true")
		},
	})
	defer restore()

	if err := RunScript(dir, "npm run build\n", &bytes.Buffer{}); err != nil {
		t.Fatalf("RunScript: %v", err)
	}

	if got.version != "22" {
		t.Errorf("ran under Node %q, want the site's 22", got.version)
	}
	// The whole script runs under that version, not just its first command: a
	// script is several commands and the second one needs the same Node.
	if got.bin != "sh" {
		t.Errorf("bin = %q, want the shell so the whole script is covered", got.bin)
	}
	if len(got.args) < 3 || got.args[0] != "-e" || !strings.Contains(got.args[2], "npm run build") {
		t.Errorf("args = %v, want the script under sh -e -c", got.args)
	}
}

// A site that pins a version servlo does not have fails, loudly, rather than
// quietly building with a different one. A deploy is not the moment to download
// a toolchain either, so it says what to run instead of doing it.
func TestRunScript_RefusesAMissingPinnedVersion(t *testing.T) {
	restore := stubNode(t, nodeStub{available: true, installed: []string{"20"}, version: "22", pinned: "22"})
	defer restore()

	err := RunScript(t.TempDir(), "npm run build\n", &bytes.Buffer{})

	if err == nil {
		t.Fatal("a deploy ran with a Node version the site did not pin")
	}
	if !strings.Contains(err.Error(), "22") || !strings.Contains(err.Error(), "node:install") {
		t.Errorf("error = %q, does not name the version or how to install it", err)
	}
}

// A site with no Node at all still deploys. Most WordPress sites never run a
// build, and a PHP-only script must not need a Node manager to exist.
func TestRunScript_NoNodeStillRuns(t *testing.T) {
	for _, stub := range []nodeStub{
		{available: false},
		{available: true, version: ""},
	} {
		restore := stubNode(t, stub)
		var out bytes.Buffer
		err := RunScript(t.TempDir(), "echo built\n", &out)
		restore()

		if err != nil {
			t.Errorf("%+v: RunScript: %v", stub, err)
		}
		if !strings.Contains(out.String(), "built") {
			t.Errorf("%+v: the script did not run: %q", stub, out.String())
		}
	}
}

// The site directory decides the version, because that is where the project's
// own Node pin lives.
func TestRunScript_AsksAboutTheSiteDirectory(t *testing.T) {
	dir := t.TempDir()
	var asked string
	restore := stubNode(t, nodeStub{
		available: true, installed: []string{"22"}, version: "22",
		detect:  func(d string) (string, error) { asked = d; return "22", nil },
		command: func(string, string, []string) *exec.Cmd { return exec.Command("true") },
	})
	defer restore()

	if err := RunScript(dir, "npm run build\n", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if asked != dir {
		t.Errorf("asked about %q, want the site directory %q", asked, dir)
	}
}

// nodeStub stands in for a Node version manager so a deploy script can be run
// on a machine with no fnm.
type nodeStub struct {
	available  bool
	installed  []string
	hasDefault bool
	version    string
	pinned     string
	detect     func(dir string) (string, error)
	command    func(version, bin string, args []string) *exec.Cmd
}

func (s nodeStub) Name() string                              { return "stub" }
func (s nodeStub) Available() bool                           { return s.available }
func (s nodeStub) List() []string                            { return s.installed }
func (s nodeStub) HasDefault() bool                          { return s.hasDefault }
func (s nodeStub) Install(string) error                      { return nil }
func (s nodeStub) Uninstall(string) error                    { return nil }
func (s nodeStub) SetDefault(string) error                   { return nil }
func (s nodeStub) ApplyEnv(*exec.Cmd, []string)              {}
func (s nodeStub) ExecPrefix(string) string                  { return "" }
func (s nodeStub) ExecPrefixWithEnv(string, []string) string { return "" }
func (s nodeStub) ShimScript(string, string) string          { return "" }
func (s nodeStub) Command(version, bin string, args []string) *exec.Cmd {
	if s.command != nil {
		return s.command(version, bin, args)
	}
	return exec.Command("true")
}

func stubNode(t *testing.T, s nodeStub) func() {
	t.Helper()
	prevActive, prevDetect, prevPinned := nodeActive, nodeDetectVersion, nodePinnedVersion
	nodeActive = func() node.Manager { return s }
	nodeDetectVersion = func(dir string) (string, error) {
		if s.detect != nil {
			return s.detect(dir)
		}
		return s.version, nil
	}
	nodePinnedVersion = func(string) string { return s.pinned }
	return func() {
		nodeActive, nodeDetectVersion, nodePinnedVersion = prevActive, prevDetect, prevPinned
	}
}

// The deploy script is the thing that runs `npm run build`, so it is the thing
// that has to be confined. An unconfined build is the one that takes MySQL with
// it when it runs out of memory.
func TestRunScript_RunsInsideAMemoryCappedScope(t *testing.T) {
	defer stubNode(t, nodeStub{available: false})()
	var got struct {
		limit int64
		unit  string
		inner []string
	}
	prev := wrapScopeFn
	wrapScopeFn = func(cmd *exec.Cmd, limit int64, unit string) *exec.Cmd {
		got.limit, got.unit, got.inner = limit, unit, cmd.Args
		return cmd
	}
	defer func() { wrapScopeFn = prev }()

	if err := RunScript(t.TempDir(), "echo npm run build\n", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	if got.limit <= 0 {
		t.Errorf("the build was given no memory ceiling (%d)", got.limit)
	}
	if got.unit == "" {
		t.Error("the build was given no scope name")
	}
	if len(got.inner) == 0 || !strings.Contains(strings.Join(got.inner, " "), "npm run build") {
		t.Errorf("the wrong thing was confined: %v", got.inner)
	}
}

// The Node wrapper and the scope compose the right way round: the scope
// confines the whole thing, Node included, rather than Node wrapping a scope
// that the build then escapes.
func TestRunScript_TheScopeIsOutsideTheNodeWrapper(t *testing.T) {
	defer stubNode(t, nodeStub{
		available: true, installed: []string{"22"}, version: "22",
		command: func(version, bin string, args []string) *exec.Cmd {
			return exec.Command("fnm-stub", append([]string{"exec", version, bin}, args...)...)
		},
	})()
	var confined []string
	prev := wrapScopeFn
	wrapScopeFn = func(cmd *exec.Cmd, _ int64, _ string) *exec.Cmd {
		confined = cmd.Args
		return exec.Command("true")
	}
	defer func() { wrapScopeFn = prev }()

	if err := RunScript(t.TempDir(), "npm run build\n", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	if len(confined) == 0 || confined[0] != "fnm-stub" {
		t.Errorf("the scope confined %v, want the Node-wrapped command", confined)
	}
}

// Running out of memory is the one build failure whose cause the operator has
// to be told, because the fix is a bigger droplet or a smaller build rather
// than pressing deploy again. A bare exit code says none of that.
//
// Both codes, because a scope stops a build either way: 137 when the kernel
// SIGKILLed the task it picked, 143 when systemd then SIGTERMed the rest of the
// scope. The operator is in the same situation and reads the same sentence.
func TestRunScript_SaysWhenTheBuildRanOutOfMemory(t *testing.T) {
	for _, code := range []string{"137", "143"} {
		defer stubNode(t, nodeStub{available: false})()
		prev := wrapScopeFn
		wrapScopeFn = func(cmd *exec.Cmd, _ int64, _ string) *exec.Cmd {
			return exec.Command("sh", "-c", "exit "+code)
		}

		err := RunScript(t.TempDir(), "npm run build\n", &bytes.Buffer{})
		wrapScopeFn = prev

		if err == nil {
			t.Fatalf("a build stopped with %s reported success", code)
		}
		said := strings.ToLower(err.Error())
		if !strings.Contains(said, "memory") {
			t.Errorf("exit %s: error = %q, does not say it ran out of memory", code, err)
		}
		if !strings.Contains(said, "mb") {
			t.Errorf("exit %s: error = %q, does not name the ceiling it hit", code, err)
		}
	}
}

// On a machine with no user manager the build runs unconfined, and then those
// same signals are not a ceiling being hit, they are somebody having stopped
// the deploy. Saying "out of memory" there sends them after a machine size that
// was never the problem.
func TestRunScript_DoesNotBlameMemoryForAnUnconfinedBuild(t *testing.T) {
	defer stubNode(t, nodeStub{available: false})()
	prev := wrapScopeFn
	// What Wrap does when it cannot make a scope: hands the command straight
	// back.
	wrapScopeFn = func(cmd *exec.Cmd, _ int64, _ string) *exec.Cmd { return cmd }
	defer func() { wrapScopeFn = prev }()

	err := RunScript(t.TempDir(), "kill -TERM $$; sleep 5\n", &bytes.Buffer{})

	if err == nil {
		t.Fatal("the shell survived being terminated")
	}
	if strings.Contains(strings.ToLower(err.Error()), "memory") {
		t.Errorf("error = %q, blamed memory for a build nothing was confining", err)
	}
}

// managedSite stages a site on a database servlo does not host: a connection in
// the registry and an .env pointing at the provider's own hostname, which is
// what the env injection writes for one.
func managedSite(t *testing.T) *config.Site {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reg := &dbconn.Registry{Connections: []dbconn.Connection{
		dbconn.External("managed", "mysql", "db-mysql-nyc3-1234.b.db.example.com", 25060, "doadmin", "secret"),
	}}
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := "DB_CONNECTION=mysql\nDB_HOST=db-mysql-nyc3-1234.b.db.example.com\nDB_PORT=25060\nDB_DATABASE=shop\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: dir, Database: "managed"}
}

// A managed database has no container to dump inside, and the deploy stops when
// the backup fails. Without a path for one, a site on a managed database could
// not run a migrating deploy at all.
func TestSnapshot_BacksUpADatabaseServloDoesNotHost(t *testing.T) {
	site := managedSite(t)
	var asked string
	prev := remoteDump
	remoteDump = func(conn dbconn.Connection, database string, w io.Writer) error {
		asked = conn.Host + "/" + database
		_, err := io.WriteString(w, "-- the dump\n")
		return err
	}
	t.Cleanup(func() { remoteDump = prev })

	name, err := Snapshot(site)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if asked != "db-mysql-nyc3-1234.b.db.example.com/shop" {
		t.Errorf("dumped %q, not the site's own connection and database", asked)
	}

	dump := filepath.Join(config.SnapshotsDir(), "managed", "databases", "shop", name, "dump.sql.gz")
	f, err := os.Open(dump)
	if err != nil {
		t.Fatalf("the backup is not in the snapshot store: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("the stored dump is not gzipped like a local one: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading the stored dump: %v", err)
	}
	if string(body) != "-- the dump\n" {
		t.Errorf("stored %q", body)
	}
}

// A dump that fails halfway is not a backup, and the deploy has to hear about
// it rather than being told a name for something that is not there.
func TestSnapshot_ReportsAFailedRemoteDump(t *testing.T) {
	site := managedSite(t)
	prev := remoteDump
	remoteDump = func(dbconn.Connection, string, io.Writer) error {
		return errors.New("the server closed the connection")
	}
	t.Cleanup(func() { remoteDump = prev })

	name, err := Snapshot(site)
	if err == nil {
		t.Fatal("a failed dump was reported as a backup")
	}
	if name != "" {
		t.Errorf("a name was returned for a backup that was not taken: %q", name)
	}
}

// Most sites on a PHP server never build anything with Node. They pin no
// version, so the one servlo hands them is its own global default, and on a
// server where nobody ever ran `servlo node:install` that version is not there.
// Refusing the deploy over it means a WordPress site whose script is two lines
// of PHP cannot deploy at all.
func TestRunScript_ASiteThatNeverAsksForNodeStillDeploys(t *testing.T) {
	defer stubNode(t, nodeStub{
		available: true, installed: []string{"20"}, hasDefault: true, version: "22",
	})()

	if err := RunScript(t.TempDir(), "php artisan optimize\n", &bytes.Buffer{}); err != nil {
		t.Fatalf("a site with no Node pin was refused its deploy: %v", err)
	}
}

// A site that did pin a version and did not get it is a different case: the
// build it is about to run needs that toolchain, so it is told rather than run
// against whatever else happens to be installed.
func TestRunScript_APinnedVersionThatIsMissingIsStillRefused(t *testing.T) {
	defer stubNode(t, nodeStub{
		available: true, installed: []string{"20"}, hasDefault: true, version: "22", pinned: "22",
	})()

	err := RunScript(t.TempDir(), "npm run build\n", &bytes.Buffer{})
	if err == nil {
		t.Fatal("a build pinned to a Node version that is not installed ran anyway")
	}
	if !strings.Contains(err.Error(), "node:install 22") {
		t.Errorf("the refusal does not say how to fix it: %v", err)
	}
}
