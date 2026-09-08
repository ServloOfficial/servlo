package serviceops

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/freeport"
)

// freePort returns a port nothing currently holds, searching upward from start.
//
// These tests used to name ports outright. That made them fail whenever anything
// else on the machine happened to be holding one, which turned pull requests red
// for reasons having nothing to do with their changes — and the production path
// right here already handles a taken port by shifting to the next one, so the
// tests were stricter about the environment than the code they cover.
func freePort(t *testing.T, start int) int {
	t.Helper()
	p := freeport.FirstFree(start, func(port int) bool { return !freeport.Bindable(port) })
	if p == 0 {
		t.Skipf("no free port at or above %d on this machine", start)
	}
	return p
}

// The guard's shift hook is silenced only while a SetPublishedPort window is
// open, counted so overlapping windows both hold it, while the forced fire
// (SetPublishedPort's own end-of-change refresh) always runs. This is the
// non-racy replacement for nil-swapping the package-global hook.
func TestPublishedPortShiftSuppression(t *testing.T) {
	var fired []int
	prev := OnPublishedPortShift
	OnPublishedPortShift = func(_ string, port int) { fired = append(fired, port) }
	t.Cleanup(func() { OnPublishedPortShift = prev })

	firePublishedPortShift("mysql", 1) // not suppressed: fires
	unsuppress := suppressPublishedPortShift()
	firePublishedPortShift("mysql", 2)       // suppressed: silenced
	firePublishedPortShiftForced("mysql", 3) // forced: fires despite suppression
	un2 := suppressPublishedPortShift()      // nested window
	unsuppress()                             // one window closed, still suppressed
	firePublishedPortShift("mysql", 4)       // silenced
	un2()                                    // all windows closed
	firePublishedPortShift("mysql", 5)       // fires again

	if want := []int{1, 3, 5}; !reflect.DeepEqual(fired, want) {
		t.Errorf("fired = %v, want %v", fired, want)
	}
}

func TestValidateExtraPort(t *testing.T) {
	cases := []struct {
		spec string
		ok   bool
	}{
		{"3411", true},
		{"3411:3306", true},
		{"127.0.0.1:3411:3306", true},
		{"3411:3306/tcp", true},
		{"53:53/udp", true},
		{"", false},
		{"nope", false},
		{"3411:bad", false},
		{"70000:3306", false},
		{"-1:3306", false},
		{"a:b:c:d", false},
	}
	for _, c := range cases {
		err := ValidateExtraPort(c.spec)
		if c.ok && err != nil {
			t.Errorf("ValidateExtraPort(%q) = %v, want nil", c.spec, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateExtraPort(%q) = nil, want error", c.spec)
		}
	}
}

func TestRemovePort(t *testing.T) {
	// Full spec removes every mapping on that host port.
	got := removePort([]string{"3411:3306", "39580:80", "3411:3306"}, "3411:3306")
	if len(got) != 1 || got[0] != "39580:80" {
		t.Errorf("removePort(full spec) dropped wrong entries: %v", got)
	}
	// A bare host port removes the mapping too, so `expose --remove 39580` works.
	got = removePort([]string{"3411:3306", "39580:80"}, "39580")
	if len(got) != 1 || got[0] != "3411:3306" {
		t.Errorf("removePort(host only) should drop 39580:80: %v", got)
	}
	// An unparseable target removes nothing.
	if got := removePort([]string{"39580:80"}, ""); len(got) != 1 {
		t.Errorf("removePort(empty) should keep everything: %v", got)
	}
}

// TestSetPublishedPortRange rejects out-of-range ports before touching config.
func TestSetPublishedPortRange(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if _, err := SetPublishedPort("mysql", 70000); err == nil {
		t.Fatal("SetPublishedPort(70000) = nil, want range error")
	}
}

// TestSetPublishedPortNotInstalled saves the override for a built-in service that
// isn't installed and never resurrects the unit.
func TestSetPublishedPortNotInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	port := freePort(t, 33991)
	res, err := SetPublishedPort("mysql", port)
	if err != nil {
		t.Fatalf("SetPublishedPort: %v", err)
	}
	if res.Installed {
		t.Error("Installed = true for an uninstalled service")
	}
	if res.Actual != port {
		t.Errorf("Actual = %d, want %d", res.Actual, port)
	}
	if config.ServicePublishedPort("mysql") != port {
		t.Errorf("override not persisted, got %d", config.ServicePublishedPort("mysql"))
	}
}

// TestSetPublishedPortNoOp reports NoOp when the requested port already matches
// the saved override and doesn't error.
func TestSetPublishedPortNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	port := freePort(t, 33991)
	if _, err := SetPublishedPort("mysql", port); err != nil {
		t.Fatalf("first SetPublishedPort: %v", err)
	}
	res, err := SetPublishedPort("mysql", port)
	if err != nil {
		t.Fatalf("second SetPublishedPort: %v", err)
	}
	if !res.NoOp {
		t.Error("NoOp = false, want true on repeat of the same port")
	}
}

// TestSetExtraPortsDedup persists a de-duplicated, validated set for a built-in
// service and rejects a malformed mapping.
func TestSetExtraPortsDedup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := SetExtraPorts("mysql", []string{"39580:80", "39580:80", " "}); err != nil {
		t.Fatalf("SetExtraPorts: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Services["mysql"].ExtraPorts
	if len(got) != 1 || got[0] != "39580:80" {
		t.Errorf("ExtraPorts = %v, want [39580:80]", got)
	}
	if err := SetExtraPorts("mysql", []string{"bad"}); err == nil {
		t.Error("SetExtraPorts(bad) = nil, want validation error")
	}
}

// TestAddRemoveExtraPort adds then removes a single mapping for a built-in service.
func TestAddRemoveExtraPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := AddExtraPort("mysql", "39590:9000"); err != nil {
		t.Fatalf("AddExtraPort: %v", err)
	}
	if cfg, _ := config.LoadGlobal(); len(cfg.Services["mysql"].ExtraPorts) != 1 {
		t.Fatalf("AddExtraPort did not persist: %v", cfg.Services["mysql"].ExtraPorts)
	}
	if err := RemoveExtraPort("mysql", "39590:9000"); err != nil {
		t.Fatalf("RemoveExtraPort: %v", err)
	}
	if cfg, _ := config.LoadGlobal(); len(cfg.Services["mysql"].ExtraPorts) != 0 {
		t.Errorf("RemoveExtraPort left entries: %v", cfg.Services["mysql"].ExtraPorts)
	}
}

func TestSetExtraPortsRejectsCustomName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("not-a-preset", []string{"39580:80"}); err == nil {
		t.Error("SetExtraPorts on a non-preset = nil, want error")
	}
}

// TestSetExtraPortsOptionalPreset persists extra ports for an optional (non
// default-stack) preset like gotenberg: it's a service we ship, so the gate is
// preset ownership, not the default flag.
func TestSetExtraPortsOptionalPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("gotenberg", []string{"39580:80"}); err != nil {
		t.Fatalf("SetExtraPorts(gotenberg): %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["gotenberg"].ExtraPorts; len(got) != 1 || got[0] != "39580:80" {
		t.Errorf("gotenberg ExtraPorts = %v, want [39580:80]", got)
	}
}

// TestSetPublishedPortRejectsSiblingPort refuses a port another servlo service
// already claims (postgres's default 5432) even while that sibling is stopped.
func TestSetPublishedPortRejectsSiblingPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	_, err := SetPublishedPort("mysql", 5432)
	if !errors.Is(err, ErrPortReserved) {
		t.Fatalf("SetPublishedPort(mysql, 5432) err = %v, want ErrPortReserved", err)
	}
}

// TestSetPublishedPortRejectsOwnExtraPort refuses a published port that already
// serves as one of the same service's extra ports.
func TestSetPublishedPortRejectsOwnExtraPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("mysql", []string{"33950:3306"}); err != nil {
		t.Fatal(err)
	}
	_, err := SetPublishedPort("mysql", 33950)
	if !errors.Is(err, ErrPortInUse) {
		t.Fatalf("SetPublishedPort onto own extra port err = %v, want ErrPortInUse", err)
	}
}

// TestSetExtraPortsRejectsMainPort refuses an extra mapping that republishes the
// service's own main host port.
func TestSetExtraPortsRejectsMainPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("mysql", []string{"3306:3306"}); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("SetExtraPorts onto main port err = %v, want ErrPortInUse", err)
	}
}

// TestSetExtraPortsRejectsSiblingPort refuses an extra mapping on a host port
// another servlo service already claims.
func TestSetExtraPortsRejectsSiblingPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("mysql", []string{"5432:5432"}); !errors.Is(err, ErrPortReserved) {
		t.Fatalf("SetExtraPorts onto postgres's port err = %v, want ErrPortReserved", err)
	}
}

// TestSetPublishedPortDefaultResetsNotCollides pins finding #4: asking for the
// preset default normalises to a reset (override 0) instead of erroring as
// "port already in use" — a running service holds its own default port, so the
// old bind probe rejected `servlo service port mysql 3306` while mysql owned 3306.
func TestSetPublishedPortDefaultResetsNotCollides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := SetPublishedPort("mysql", freePort(t, 33000)); err != nil {
		t.Fatalf("move off default: %v", err)
	}
	res, err := SetPublishedPort("mysql", 3306) // mysql's preset default
	if err != nil {
		t.Fatalf("requesting the preset default must not error, got %v", err)
	}
	if got := config.ServicePublishedPort("mysql"); got != 0 {
		t.Errorf("requesting the default must reset the override to 0, got %d", got)
	}
	_ = res
}

// TestSetPublishedPortDefaultNoOpWhenAlreadyDefault: a service already on its
// default reports NoOp when asked for that same default port, never an error.
func TestSetPublishedPortDefaultNoOpWhenAlreadyDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	res, err := SetPublishedPort("mysql", 3306)
	if err != nil {
		t.Fatalf("requesting the default on a default service must not error, got %v", err)
	}
	if !res.NoOp {
		t.Error("requesting the current default must be a NoOp")
	}
}

// TestSetPublishedPortRollsBackOnStartFailure pins finding #3: when the restart
// on the new port fails, the service is brought back up on its previous port and
// the config is rolled back, instead of left down with the override already moved.
func TestSetPublishedPortRollsBackOnStartFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	fakeQuadletOnDisk(t, "mysql") // ServiceInstalled -> true

	prevStatus, prevStop, prevStart := portsUnitStatus, portsStopUnit, portsStartUnit
	prevWait, prevRerender := portsWaitReady, portsRerender
	t.Cleanup(func() {
		portsUnitStatus, portsStopUnit, portsStartUnit = prevStatus, prevStop, prevStart
		portsWaitReady, portsRerender = prevWait, prevRerender
	})

	startCalls := 0
	portsUnitStatus = func(string) (string, error) { return "active", nil }
	portsStopUnit = func(string) error { return nil }
	portsRerender = func(string) error { return nil }
	portsWaitReady = func(string, time.Duration) error { return nil }
	portsStartUnit = func(string) error {
		startCalls++
		if startCalls == 1 {
			return errors.New("address already in use")
		}
		return nil // the rollback restart on the previous port succeeds
	}

	if _, err := SetPublishedPort("mysql", freePort(t, 33000)); err == nil {
		t.Fatal("a failed start must surface an error so the caller knows the change didn't take")
	}
	if startCalls != 2 {
		t.Fatalf("expected a rollback restart attempt after the failed start, startCalls=%d", startCalls)
	}
	if got := config.ServicePublishedPort("mysql"); got != 0 {
		t.Errorf("config must roll back to the previous port (0=default), got %d", got)
	}
}

// TestSetPublishedPortForSecondary moves a multi-port service's secondary mapping
// (rustfs' 9001 console) and persists it under PublishedPorts keyed by container port,
// leaving the primary untouched.
func TestSetPublishedPortForSecondary(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	res, err := SetPublishedPortFor("rustfs", 9001, 39002)
	if err != nil {
		t.Fatalf("SetPublishedPortFor: %v", err)
	}
	if res.Actual != 39002 {
		t.Errorf("Actual = %d, want 39002", res.Actual)
	}
	if got := config.ServicePublishedPorts("rustfs")[9001]; got != 39002 {
		t.Errorf("PublishedPorts[9001] = %d, want 39002", got)
	}
	if config.ServicePublishedPort("rustfs") != 0 {
		t.Error("moving a secondary must not touch the primary PublishedPort")
	}
}

// TestSetPublishedPortForReset clears a secondary override when asked for its
// preset default, and reports NoOp on a repeat of the same override.
func TestSetPublishedPortForReset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := SetPublishedPortFor("rustfs", 9001, 39002); err != nil {
		t.Fatalf("initial move: %v", err)
	}
	repeat, err := SetPublishedPortFor("rustfs", 9001, 39002)
	if err != nil || !repeat.NoOp {
		t.Errorf("repeat = %+v, err %v, want NoOp", repeat, err)
	}
	if _, err := SetPublishedPortFor("rustfs", 9001, 9001); err != nil { // the preset default
		t.Fatalf("reset to default: %v", err)
	}
	if got := config.ServicePublishedPorts("rustfs"); len(got) != 0 {
		t.Errorf("requesting the default must clear the override, got %v", got)
	}
}

// TestSetPublishedPortForPrimaryDelegates: targeting the primary mapping's
// container port routes through SetPublishedPort so the primary keeps its own
// override field and guard handling.
func TestSetPublishedPortForPrimaryDelegates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := SetPublishedPortFor("rustfs", 9000, 49000); err != nil {
		t.Fatalf("SetPublishedPortFor primary: %v", err)
	}
	if config.ServicePublishedPort("rustfs") != 49000 {
		t.Errorf("primary override = %d, want 49000", config.ServicePublishedPort("rustfs"))
	}
	if len(config.ServicePublishedPorts("rustfs")) != 0 {
		t.Error("primary move must not write the secondary map")
	}
}

// TestSetPublishedPortForRejects covers the guard rails: an unknown container
// port, an out-of-range host port, and colliding a secondary onto the primary's port.
func TestSetPublishedPortForRejects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := SetPublishedPortFor("rustfs", 9999, 39002); err == nil {
		t.Error("unknown container port = nil, want error")
	}
	if _, err := SetPublishedPortFor("rustfs", 9001, 70000); err == nil {
		t.Error("out-of-range host port = nil, want error")
	}
	if _, err := SetPublishedPortFor("rustfs", 9001, 9000); !errors.Is(err, ErrPortInUse) {
		t.Errorf("secondary onto primary's port err = %v, want ErrPortInUse", err)
	}
}

// TestSetPublishedPortForRollsBackOnStartFailure: a secondary move whose restart
// can't bind the new port restores the previous override and brings the unit back
// up, instead of leaving it down with the config already moved — the same recovery
// the primary path has, which the secondary path previously lacked.
func TestSetPublishedPortForRollsBackOnStartFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	fakeQuadletOnDisk(t, "rustfs") // ServiceInstalled -> true

	prevStatus, prevStop, prevStart := portsUnitStatus, portsStopUnit, portsStartUnit
	prevWait, prevRerender := portsWaitReady, portsRerender
	t.Cleanup(func() {
		portsUnitStatus, portsStopUnit, portsStartUnit = prevStatus, prevStop, prevStart
		portsWaitReady, portsRerender = prevWait, prevRerender
	})

	startCalls := 0
	portsUnitStatus = func(string) (string, error) { return "active", nil }
	portsStopUnit = func(string) error { return nil }
	portsRerender = func(string) error { return nil }
	portsWaitReady = func(string, time.Duration) error { return nil }
	portsStartUnit = func(string) error {
		startCalls++
		if startCalls == 1 {
			return errors.New("address already in use")
		}
		return nil // the rollback restart on the previous port succeeds
	}

	if _, err := SetPublishedPortFor("rustfs", 9001, 39002); err == nil {
		t.Fatal("a failed start must surface an error so the caller knows the move didn't take")
	}
	if startCalls != 2 {
		t.Fatalf("expected a rollback restart attempt after the failed start, startCalls=%d", startCalls)
	}
	if got := config.ServicePublishedPorts("rustfs"); len(got) != 0 {
		t.Errorf("config must roll the secondary override back (had none), got %v", got)
	}
}

// TestPortReservedBySecondaryDefaultOfOther pins finding C: a stopped multi-port
// service reserves its un-overridden secondary DEFAULT port too, so another
// service can't be assigned onto it and collide at boot (rustfs' 9001 console).
func TestPortReservedBySecondaryDefaultOfOther(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if _, err := SetPublishedPort("redis", 9001); !errors.Is(err, ErrPortReserved) {
		t.Fatalf("SetPublishedPort(redis, 9001) err = %v, want ErrPortReserved (rustfs' secondary default)", err)
	}
}

// TestSetPublishedPortForResetNormalizesRequested pins finding 5: resetting a
// secondary by passing the mapping's default reports Requested 0, so callers print
// "cleared the override" rather than "saved published port 9001".
func TestSetPublishedPortForResetNormalizesRequested(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if _, err := SetPublishedPortFor("rustfs", 9001, 39002); err != nil {
		t.Fatalf("initial move: %v", err)
	}
	res, err := SetPublishedPortFor("rustfs", 9001, 9001) // pass the preset default
	if err != nil {
		t.Fatalf("reset via default: %v", err)
	}
	if res.Requested != 0 {
		t.Errorf("Requested = %d, want 0 (passing the default is a reset)", res.Requested)
	}
	if got := config.ServicePublishedPorts("rustfs"); len(got) != 0 {
		t.Errorf("override must be cleared, got %v", got)
	}
}

// TestSnapshotRestorePublishedPorts: the ports-modal transaction can capture a
// service's published-port config and roll it back wholesale after a partial save.
func TestSnapshotRestorePublishedPorts(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Both setters bind-probe the host port, so naming one outright fails
	// whenever anything on the machine is holding it, and every port this test
	// used to name sat inside the ephemeral range a busy runner allocates from.
	primary, secondary := freePort(t, 49000), freePort(t, 39002)
	moved := freePort(t, primary+1)

	if _, err := SetPublishedPort("rustfs", primary); err != nil {
		t.Fatalf("seed primary: %v", err)
	}
	if _, err := SetPublishedPortFor("rustfs", 9001, secondary); err != nil {
		t.Fatalf("seed secondary: %v", err)
	}
	snap, ok := SnapshotPublishedPorts("rustfs")
	if !ok {
		t.Fatal("SnapshotPublishedPorts returned !ok")
	}

	// Mutate away from the snapshot, then restore.
	if _, err := SetPublishedPort("rustfs", moved); err != nil {
		t.Fatalf("mutate primary: %v", err)
	}
	if _, err := SetPublishedPortFor("rustfs", 9001, 9001); err != nil {
		t.Fatalf("mutate secondary: %v", err)
	}
	if err := RestorePublishedPorts("rustfs", snap); err != nil {
		t.Fatalf("RestorePublishedPorts: %v", err)
	}
	if got := config.ServicePublishedPort("rustfs"); got != primary {
		t.Errorf("primary not restored, got %d want %d", got, primary)
	}
	if got := config.ServicePublishedPorts("rustfs")[9001]; got != secondary {
		t.Errorf("secondary not restored, got %d want %d", got, secondary)
	}
}

func TestErrPortInUseSentinel(t *testing.T) {
	err := errors.Join(ErrPortInUse)
	if !errors.Is(err, ErrPortInUse) {
		t.Error("ErrPortInUse not matchable via errors.Is")
	}
}

// A ports-modal save that fails partway through rolls back with RestorePublishedPorts.
// The forward path fired the host-proxy .env refresh to the new port, so the
// rollback must re-fire it to the restored port, or host-proxy sites are left
// pointing at a port the service no longer publishes.
func TestRestorePublishedPorts_RefreshesHostProxyToRestoredPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	fakeQuadletOnDisk(t, "mysql") // ServiceInstalled -> true

	prevStatus, prevStop, prevStart := portsUnitStatus, portsStopUnit, portsStartUnit
	prevWait, prevRerender := portsWaitReady, portsRerender
	t.Cleanup(func() {
		portsUnitStatus, portsStopUnit, portsStartUnit = prevStatus, prevStop, prevStart
		portsWaitReady, portsRerender = prevWait, prevRerender
	})
	portsUnitStatus = func(string) (string, error) { return "active", nil }
	portsStopUnit = func(string) error { return nil }
	portsStartUnit = func(string) error { return nil }
	portsWaitReady = func(string, time.Duration) error { return nil }
	portsRerender = func(string) error { return nil }

	var fired []int
	prevHook := OnPublishedPortShift
	OnPublishedPortShift = func(_ string, port int) { fired = append(fired, port) }
	t.Cleanup(func() { OnPublishedPortShift = prevHook })

	seeded := freePort(t, 33000)
	if _, err := SetPublishedPort("mysql", seeded); err != nil {
		t.Fatalf("seed: %v", err)
	}
	snap, ok := SnapshotPublishedPorts("mysql")
	if !ok {
		t.Fatal("snapshot !ok")
	}
	// The apply moves the primary and refreshes host-proxy .env to the new port.
	if _, err := SetPublishedPort("mysql", freePort(t, seeded+1)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	fired = nil
	if err := RestorePublishedPorts("mysql", snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := config.ServicePublishedPort("mysql"); got != seeded {
		t.Fatalf("port not restored, got %d want %d", got, seeded)
	}
	// Exactly one refresh, carrying the restored port, so host-proxy .env follows.
	if len(fired) != 1 || fired[0] != seeded {
		t.Errorf("rollback must refresh host-proxy sites to the restored port; fired=%v want [%d]", fired, seeded)
	}
}
