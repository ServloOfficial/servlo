package buildscope

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The cap leaves the rest of the droplet room to keep running. A build allowed
// the whole machine is a build that takes MySQL with it, which is the entire
// point of capping it.
func TestLimitFor_LeavesRoomForEverythingElse(t *testing.T) {
	// Exact, not a range. A range wide enough to be comfortable is a range that
	// still passes with the floor or the ceiling deleted.
	cases := []struct{ totalMB, wantMB int64 }{
		// Half, once there is enough to halve.
		{2048, 1024},
		{4096, 2048},
		// Under the floor: a cap below this builds nothing, and refusing to
		// build is not an improvement on failing under load.
		{512, minLimit / MB},
		{768, minLimit / MB},
		// Over the ceiling: past a few gigabytes a build is not hungry, it is
		// broken, and a bigger machine should not hide that for longer.
		{16384, maxLimit / MB},
		{65536, maxLimit / MB},
	}
	for _, c := range cases {
		if got := LimitFor(c.totalMB * MB); got != c.wantMB*MB {
			t.Errorf("%dMB host: cap %dMB, want %dMB", c.totalMB, got/MB, c.wantMB)
		}
	}

	// And the cap always leaves the rest of the machine something to run in.
	for _, totalMB := range []int64{2048, 4096, 16384} {
		if got := LimitFor(totalMB * MB); got >= totalMB*MB {
			t.Errorf("%dMB host: cap %dMB leaves nothing for the services", totalMB, got/MB)
		}
	}
}

// A bigger machine gets a bigger cap, which is what makes this a fraction
// rather than a number somebody has to tune.
func TestLimitFor_ScalesWithTheMachine(t *testing.T) {
	small, large := LimitFor(2048*MB), LimitFor(8192*MB)
	if large <= small {
		t.Errorf("cap did not grow with the machine: %dMB then %dMB", small/MB, large/MB)
	}
}

// A host servlo cannot measure gets a conservative cap rather than none. No cap
// is the behaviour this story exists to remove.
func TestLimitFor_UnknownHostStillCaps(t *testing.T) {
	for _, total := range []int64{0, -1} {
		got := LimitFor(total)
		if got <= 0 {
			t.Errorf("total %d gave no cap at all", total)
		}
		if got > 1024*MB {
			t.Errorf("total %d gave an optimistic %dMB cap", total, got/MB)
		}
	}
}

// The scope is what confines the build. Every part of it is load bearing, so
// the arguments are asserted rather than assumed.
func TestWrap_BuildsAScopedCommand(t *testing.T) {
	defer withScope(t)()

	inner := exec.Command("sh", "-e", "-c", "npm run build")
	got := Wrap(inner, 512*MB, "servlo-build-shop")

	if got == inner {
		t.Fatal("the command was not wrapped")
	}
	line := strings.Join(got.Args, " ")
	for _, want := range []string{
		"systemd-run", "--user", "--scope",
		"MemoryMax=536870912",
		// Without this the build swaps instead of failing, which drags the whole
		// droplet down slowly rather than failing one deploy quickly.
		"MemorySwapMax=0",
		// Or the transient unit is left behind on every build.
		"--collect",
		"--unit=servlo-build-shop",
		"sh -e -c npm run build",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("scoped command is missing %q:\n%s", want, line)
		}
	}
}

// The wrapped command keeps everything the caller set on the original, or the
// build runs in the wrong directory with the wrong environment and its output
// goes nowhere.
func TestWrap_KeepsTheCallersSetup(t *testing.T) {
	defer withScope(t)()

	inner := exec.Command("sh", "-c", "true")
	inner.Dir = "/srv/shop"
	inner.Env = []string{"PATH=/usr/bin", "NODE_ENV=production"}

	got := Wrap(inner, 512*MB, "servlo-build-shop")

	if got.Dir != "/srv/shop" {
		t.Errorf("Dir = %q, lost the site directory", got.Dir)
	}
	if strings.Join(got.Env, " ") != strings.Join(inner.Env, " ") {
		t.Errorf("Env = %v, lost the caller's environment", got.Env)
	}
}

// No systemd user session, no scope, and the build still runs. A container or a
// machine without a user manager must not lose the ability to deploy.
func TestWrap_UnscopedWhenSystemdIsUnavailable(t *testing.T) {
	prev := scopeAvailable
	scopeAvailable = func() bool { return false }
	defer func() { scopeAvailable = prev }()

	inner := exec.Command("sh", "-c", "true")
	got := Wrap(inner, 512*MB, "servlo-build-shop")

	if got != inner {
		t.Errorf("wrapped anyway: %v", got.Args)
	}
}

// A cap of zero or less means the caller could not work one out, and running
// unconfined is better than a scope that refuses everything.
func TestWrap_NoLimitMeansNoScope(t *testing.T) {
	defer withScope(t)()

	inner := exec.Command("sh", "-c", "true")
	if got := Wrap(inner, 0, "servlo-build-shop"); got != inner {
		t.Errorf("a zero cap still built a scope: %v", got.Args)
	}
}

// Both shapes a memory kill takes, because a build stopped at its ceiling
// arrives as either and the operator has to be told the same thing regardless.
// The kernel SIGKILLs the task it picks inside the cgroup; systemd then stops
// the scope under its OOM policy, which SIGTERMs whatever is left. Each reaches
// the caller as a signal directly, or as 128 plus the signal through a shell.
//
// Not theory. The oversized-build test below skips on a container with no user
// manager, so CI was the first machine to run it for real, and the kill it
// reported was SIGTERM.
func TestOutOfMemory_RecognisesBothShapesOfAScopeKill(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGKILL, syscall.SIGTERM} {
		if err := signalledErr(t, sig); !OutOfMemory(err) {
			t.Errorf("a build killed with %v was not recognised: %v", sig, err)
		}
	}
	for _, code := range []int{137, 143} {
		if !OutOfMemory(exitErr(t, code)) {
			t.Errorf("exit %d was not recognised as the scope stopping the build", code)
		}
	}

	// And nothing else is. Every other failure is something in the code, and
	// telling the operator to buy a bigger droplet would send them away from it.
	// A scope stops a build with those two signals and no others, so a build
	// that died of anything else died of something the operator can fix.
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP} {
		if err := signalledErr(t, sig); OutOfMemory(err) {
			t.Errorf("a build killed with %v was reported as out of memory", sig)
		}
	}
	for _, code := range []int{1, 2, 127} {
		if OutOfMemory(exitErr(t, code)) {
			t.Errorf("exit %d was reported as out of memory", code)
		}
	}
	if OutOfMemory(nil) {
		t.Error("success was reported as out of memory")
	}
}

// The scope name is per site, so two sites building at once are two scopes and
// one does not collide with, or collect, the other.
func TestUnitName_IsPerSiteAndUsable(t *testing.T) {
	a, b := UnitName("shop"), UnitName("blog")
	if a == b {
		t.Error("two sites share a scope name")
	}
	if !strings.HasPrefix(a, "servlo-build-") {
		t.Errorf("UnitName = %q, does not say what it is", a)
	}
	// A systemd unit name cannot carry a slash or a space.
	for _, bad := range []string{"/", " "} {
		if strings.Contains(UnitName("we ird/name"), bad) {
			t.Errorf("UnitName = %q contains %q", UnitName("we ird/name"), bad)
		}
	}
}

// withScope pretends this machine can start a transient scope, so the wrapping
// itself can be asserted on a container that has no user manager.
func withScope(t *testing.T) func() {
	t.Helper()
	prev := scopeAvailable
	scopeAvailable = func() bool { return true }
	return func() { scopeAvailable = prev }
}

// exitErr is a real failed command rather than a hand-built error value, so the
// test exercises the error shape a run actually produces.
func exitErr(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		t.Fatalf("expected a failure for exit %d", code)
	}
	return err
}

// signalledErr is a process that died of sig, which is what the caller sees
// when the thing it spawned is the thing that was killed.
func signalledErr(t *testing.T, sig syscall.Signal) error {
	t.Helper()
	// The sleep is the check: if the signal did not kill the shell, the command
	// succeeds and the Fatal below says so rather than the test passing on a
	// failure that never happened.
	err := exec.Command("sh", "-c", fmt.Sprintf("kill -%d $$; sleep 5", int(sig))).Run()
	if err == nil {
		t.Fatalf("the shell survived %v", sig)
	}
	return err
}

// The story's own criterion, run for real where it can be.
//
// A process that allocates past the ceiling must die inside its scope and leave
// everything outside it alone. Skipped where there is no systemd user session
// to make a scope in, which is most containers, because a test that quietly
// passed by not confining anything would be worse than one that says it did not
// run.
func TestWrap_AnOversizedBuildDiesAloneInsideItsScope(t *testing.T) {
	if !Available() {
		t.Skip("no systemd user session here, so no scope to confine anything in; this runs on a real host")
	}

	// Something outside the scope, standing in for MySQL: it has to still be
	// there afterwards.
	bystander := exec.Command("sh", "-c", "sleep 30")
	if err := bystander.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = bystander.Process.Kill()
		_, _ = bystander.Process.Wait()
	}()

	// Allocate well past the ceiling, in a way no compiler can optimise away.
	hog := exec.Command("sh", "-c", `exec head -c 400000000 /dev/zero | tail -c 400000000 >/dev/null`)
	err := Wrap(hog, 64*MB, UnitName("oversized-build-test")).Run()

	if err == nil {
		t.Fatal("a build that allocated far past its ceiling was allowed to finish")
	}
	if !OutOfMemory(err) {
		t.Errorf("the build failed for some other reason: %v", err)
	}

	// And the bystander is untouched: still running, not killed by the kernel
	// picking a victim across the whole machine.
	if err := bystander.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("something outside the scope died too: %v", err)
	}
}

// The warning fires when this machine is small and this deploy builds
// something. Both halves matter: a small machine that only pulls PHP has
// nothing to warn about, and a big machine building assets is fine.
func TestWarning(t *testing.T) {
	const build = "composer install --no-dev\nnpm ci && npm run build\n"
	const noBuild = "composer install --no-dev\nphp artisan migrate --force\n"

	cases := []struct {
		name    string
		totalMB int64
		script  string
		want    bool
	}{
		{"small machine building assets", 1024, build, true},
		{"smallest machine building assets", 512, build, true},
		{"small machine with nothing to build", 1024, noBuild, false},
		{"roomy machine building assets", 4096, build, false},
		{"roomy machine with nothing to build", 4096, noBuild, false},
		{"an empty script never warns", 1024, "", false},
		{"a commented-out build is not a build", 1024, "# npm run build\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Warning(c.totalMB*MB, c.script)
			if (got != "") != c.want {
				t.Errorf("Warning() = %q, want warning=%v", got, c.want)
			}
			if c.want {
				// It has to name the ceiling, or it is just anxiety.
				if !strings.Contains(got, "MB") {
					t.Errorf("warning names no ceiling: %q", got)
				}
			}
		})
	}
}

// Every package manager an operator might reach for, because a warning that
// only knew about npm would go quiet on the projects most likely to need it.
func TestBuildsAssets_KnowsThePackageManagers(t *testing.T) {
	for _, script := range []string{
		"npm run build", "npm ci && npm run build",
		"yarn build", "pnpm build", "bun run build",
		"  NPM_CONFIG_LOGLEVEL=error npm run build  ",
	} {
		if !buildsAssets(script) {
			t.Errorf("%q was not recognised as a build", script)
		}
	}
	for _, script := range []string{
		"", "composer install", "php artisan migrate",
		"# npm run build", "  # yarn build",
		// A word that merely contains a manager's name is not a build.
		"echo npmisgreat",
	} {
		if buildsAssets(script) {
			t.Errorf("%q was mistaken for a build", script)
		}
	}
}
