package deploy

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// harness records what a deploy did, in order, without any of it reaching a
// repository, a database or a container.
type harness struct {
	steps  []string
	out    bytes.Buffer
	gitErr map[string]error
	snapEr error
	runEr  error
	reload error
	head   string
}

func newHarness() *harness {
	return &harness{gitErr: map[string]error{}, head: "aaaa111"}
}

func (h *harness) options(site *config.Site) Options {
	return Options{
		Site: site,
		Out:  &h.out,
		Git: func(dir string, out io.Writer, args ...string) error {
			h.steps = append(h.steps, "git "+strings.Join(args, " "))
			return h.gitErr[args[0]]
		},
		Head: func(dir string) (string, error) {
			return h.head, nil
		},
		Snapshot: func(*config.Site) (string, error) {
			h.steps = append(h.steps, "snapshot")
			return "snap-1", h.snapEr
		},
		RunScript: func(dir, script string, out io.Writer) error {
			h.steps = append(h.steps, "script")
			return h.runEr
		},
		Reload: func(*config.Site) error {
			h.steps = append(h.steps, "reload")
			return h.reload
		},
		Migrates: func(*config.Site) (bool, error) { return true, nil },
		Script:   func(*config.Site) (string, error) { return "php artisan migrate --force\n", nil },
	}
}

func deploySite() *config.Site {
	return &config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", Framework: "laravel",
	}
}

// The order is the story. Back up before anything changes, pull, run the
// script, and only then make the new code live.
func TestRun_DoesThePhasesInOrder(t *testing.T) {
	h := newHarness()

	res, err := Run(h.options(deploySite()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"snapshot", "git pull --ff-only", "script", "reload"}
	if strings.Join(h.steps, ",") != strings.Join(want, ",") {
		t.Errorf("steps = %v, want %v", h.steps, want)
	}
	if res.Snapshot != "snap-1" {
		t.Errorf("Snapshot = %q, want the one it took", res.Snapshot)
	}
}

// The backup is taken before the pull, not after. A pull that fails halfway
// still leaves a database somebody may need to restore, and taking it first
// means the snapshot is of the state the site was actually in.
func TestRun_BacksUpBeforeAnythingChanges(t *testing.T) {
	h := newHarness()

	if _, err := Run(h.options(deploySite())); err != nil {
		t.Fatal(err)
	}

	snap, pull := indexOf(h.steps, "snapshot"), indexOf(h.steps, "git pull --ff-only")
	if snap < 0 || pull < 0 || snap > pull {
		t.Errorf("steps = %v, want the snapshot before the pull", h.steps)
	}
}

// A script with no migration is not schema-changing, and snapshotting every
// deploy of one would be slow enough that somebody turns the backup off.
func TestRun_SkipsTheBackupWhenNothingMigrates(t *testing.T) {
	h := newHarness()
	opts := h.options(deploySite())
	opts.Migrates = func(*config.Site) (bool, error) { return false, nil }

	res, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}

	if indexOf(h.steps, "snapshot") >= 0 {
		t.Errorf("a non-migrating deploy took a database snapshot: %v", h.steps)
	}
	if res.Snapshot != "" {
		t.Errorf("Snapshot = %q, want none", res.Snapshot)
	}
}

// If the backup cannot be taken, the deploy does not happen. The whole point
// of the guarantee is that a migration never runs without one, and continuing
// anyway would make it a guarantee only when nothing goes wrong.
func TestRun_RefusesToMigrateWhenTheBackupFailed(t *testing.T) {
	h := newHarness()
	h.snapEr = errors.New("mysql is not running")

	_, err := Run(h.options(deploySite()))

	if err == nil {
		t.Fatal("a migrating deploy went ahead after its backup failed")
	}
	if !strings.Contains(err.Error(), "mysql is not running") {
		t.Errorf("error = %q, does not carry the reason", err)
	}
	for _, step := range []string{"git pull --ff-only", "script", "reload"} {
		if indexOf(h.steps, step) >= 0 {
			t.Errorf("%q ran after the backup failed: %v", step, h.steps)
		}
	}
}

// A failed pull stops the deploy before the script runs against a tree that is
// half of one commit and half of another.
func TestRun_StopsWhenThePullFails(t *testing.T) {
	h := newHarness()
	h.gitErr["pull"] = errors.New("your local changes would be overwritten")

	_, err := Run(h.options(deploySite()))

	if err == nil {
		t.Fatal("the deploy continued after the pull failed")
	}
	if indexOf(h.steps, "script") >= 0 {
		t.Errorf("the script ran on a failed pull: %v", h.steps)
	}
}

// A failed script does not reload PHP-FPM, and that is the safe direction
// rather than an oversight. Production OPcache runs with validate_timestamps
// off, so PHP keeps serving the bytecode it already has until FPM reloads:
// withholding the reload leaves visitors on the last version that worked
// instead of the half-deployed one now on disk.
func TestRun_DoesNotMakeAHalfFinishedDeployLive(t *testing.T) {
	h := newHarness()
	h.runEr = errors.New("composer install failed")

	_, err := Run(h.options(deploySite()))

	if err == nil {
		t.Fatal("a failed script was reported as a successful deploy")
	}
	if indexOf(h.steps, "reload") >= 0 {
		t.Errorf("the new code was made live after the script failed: %v", h.steps)
	}
	if !strings.Contains(err.Error(), "composer install failed") {
		t.Errorf("error = %q, does not carry the script's own failure", err)
	}
}

// The commit either side of the pull is what a deploy history is made of, and
// what redeploying the previous one needs.
func TestRun_RecordsTheCommitItMovedFromAndTo(t *testing.T) {
	h := newHarness()
	opts := h.options(deploySite())
	first := true
	opts.Head = func(string) (string, error) {
		if first {
			first = false
			return "aaaa111", nil
		}
		return "bbbb222", nil
	}

	res, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}

	if res.FromCommit != "aaaa111" || res.ToCommit != "bbbb222" {
		t.Errorf("moved from %q to %q, want aaaa111 to bbbb222", res.FromCommit, res.ToCommit)
	}
}

// A site with an empty script still deploys: the pull is the deploy, which is
// the ordinary case on WordPress.
func TestRun_AnEmptyScriptIsAValidDeploy(t *testing.T) {
	h := newHarness()
	opts := h.options(deploySite())
	opts.Script = func(*config.Site) (string, error) { return "\n# nothing here\n", nil }
	opts.Migrates = func(*config.Site) (bool, error) { return false, nil }

	if _, err := Run(opts); err != nil {
		t.Fatalf("a site with an empty script could not deploy: %v", err)
	}
	if indexOf(h.steps, "script") >= 0 {
		t.Errorf("an empty script was run anyway: %v", h.steps)
	}
	if indexOf(h.steps, "reload") < 0 {
		t.Errorf("the pull was not made live: %v", h.steps)
	}
}

// Every phase says what it is doing before it does it, or a failure in the
// middle of a long deploy is a wall of output with no way to tell which step
// produced it.
func TestRun_NamesEachPhaseInTheOutput(t *testing.T) {
	h := newHarness()

	if _, err := Run(h.options(deploySite())); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"backup", "pull", "deploy script", "reload"} {
		if !strings.Contains(strings.ToLower(h.out.String()), want) {
			t.Errorf("the output never mentions %q:\n%s", want, h.out.String())
		}
	}
}

// A pull that brings nothing is not a failure. Re-running a deploy to pick up
// a changed script, or after fixing something by hand, is a normal thing to do.
func TestRun_SucceedsWhenThereIsNothingToPull(t *testing.T) {
	h := newHarness()
	opts := h.options(deploySite())
	opts.Head = func(string) (string, error) { return "aaaa111", nil }

	res, err := Run(opts)
	if err != nil {
		t.Fatalf("a deploy with no new commits failed: %v", err)
	}
	if res.FromCommit != res.ToCommit {
		t.Errorf("from %q to %q, want the same commit", res.FromCommit, res.ToCommit)
	}
	if indexOf(h.steps, "script") < 0 {
		t.Errorf("the script did not run: %v", h.steps)
	}
}

// A fast-forward only. A deploy that merges is a deploy that can produce a
// commit nobody reviewed, on a server, unattended.
func TestRun_PullsFastForwardOnly(t *testing.T) {
	h := newHarness()

	if _, err := Run(h.options(deploySite())); err != nil {
		t.Fatal(err)
	}

	if indexOf(h.steps, "git pull --ff-only") < 0 {
		t.Errorf("the pull was not fast-forward only: %v", h.steps)
	}
}

func indexOf(steps []string, want string) int {
	for i, s := range steps {
		if s == want {
			return i
		}
	}
	return -1
}
