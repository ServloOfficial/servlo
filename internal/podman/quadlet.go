package podman

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/systemd"
)

// quadletReloadPending records that a previous DaemonReloadIfNeeded call
// failed without being retried. The next caller forces a reload even when
// nothing else changed so a transient DBus failure does not leave
// systemd's cache stale until an external trigger heals it.
var quadletReloadPending atomic.Bool

// DaemonReloadIfNeeded reloads systemd when the caller wrote new quadlet
// content (changed=true) or when a previous reload failed and was never
// retried. Failures set a sticky flag so the next caller forces the
// retry; success clears it.
func DaemonReloadIfNeeded(changed bool) error {
	if !changed && !quadletReloadPending.Load() {
		return nil
	}
	if err := DaemonReloadFn(); err != nil {
		quadletReloadPending.Store(true)
		return err
	}
	quadletReloadPending.Store(false)
	return nil
}

// WriteQuadlet writes a Podman quadlet container unit file. Before writing
// it applies the bind policy centrally: nginx publishes on every interface
// because it serves the sites, every other container stays loopback-bound.
func WriteQuadlet(name, content string) error {
	_, err := WriteQuadletDiff(name, content)
	return err
}

// WriteQuadletDiff writes a quadlet like WriteQuadlet, but also reports
// whether the on-disk file actually changed. Callers can use this to
// daemon-reload + restart only the units that need it (e.g. servlo install
// pulling a service back from 0.0.0.0 to 127.0.0.1 on an install that
// predates the policy — without a restart the running container would
// silently keep its old bind).
func WriteQuadletDiff(name, content string) (changed bool, err error) {
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}
	autostartDisabled := false
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		autostartDisabled = cfg.Autostart.Disabled
	}
	content = BindQuadletPorts(name, content)
	content = PairIPv6Binds(content)
	content = StripInstallSection(content, autostartDisabled)
	// Centralised platform image rewrite + podman-run flags so every quadlet
	// writer emits identical units. On Apple Silicon PlatformImage swaps
	// postgis/postgis for the multi-arch imresamu/postgis (runs native, no
	// Rosetta); mysql:5.7 keeps the --platform=linux/amd64 pin.
	if svc := strings.TrimPrefix(name, "servlo-"); svc != name {
		if img := CurrentImage(content); img != "" {
			if rewritten := PlatformImage(img); rewritten != img {
				content = ApplyImage(content, rewritten)
				img = rewritten
			}
			if arg := PlatformPodmanArgs(svc, img); arg != "" {
				content = InjectPodmanArgs(content, arg)
			}
		}
	}
	path := filepath.Join(dir, name+".container")
	fileChanged := true
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		fileChanged = false
	}
	if fileChanged {
		config.GuardRealWrite(path)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return false, err
		}
	}
	return fileChanged, nil
}

// QuadletInstalled returns true if a quadlet .container file exists for the given unit name.
func QuadletInstalled(name string) bool {
	path := filepath.Join(config.QuadletDir(), name+".container")
	_, err := os.Stat(path)
	return err == nil
}

// BindQuadletPorts decides where one quadlet publishes, from what it is.
//
// Nginx binds to every interface, always. It serves the sites, and a server
// panel whose web server answers only itself is a server nobody can reach. It
// was a toggle once, off by default, which is a local development idea: a
// fresh install came up serving nothing to the internet and said nothing about
// why.
//
// Everything else stays on loopback whatever else changes, and a quadlet that
// arrives bound to every interface is pulled back rather than left. That half
// is a design law (CLAUDE.md 3.7): a database reachable from off the machine is
// a database anyone who finds the port can attack, and on a box hosting other
// people's sites there is no version of that worth the convenience.
func BindQuadletPorts(name, content string) string {
	return BindPorts(content, name == "servlo-nginx")
}

// ContainerPublishesPubliclyFn probes whether a running container currently
// publishes any port beyond loopback. It reports (public, known); known is
// false when the container is absent, stopped, or podman can't be reached.
// Swappable in tests.
var ContainerPublishesPubliclyFn = containerPublishesPublicly

func containerPublishesPublicly(name string) (bool, bool) {
	out, err := Run("ps", "--filter", "name=^"+name+"$", "--format", "{{.Ports}}")
	if err != nil {
		return false, false
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return false, false
	}
	return portsPublishToLAN(trimmed), true
}

// portsPublishToLAN parses podman's port column ("127.0.0.1:3306->3306/tcp,
// :::8025->8025/tcp, 9000/tcp") and reports whether any published binding
// listens beyond loopback. Entries without "->" are container-only ports.
func portsPublishToLAN(ports string) bool {
	for _, entry := range strings.Split(ports, ",") {
		entry = strings.TrimSpace(entry)
		hostSide, _, found := strings.Cut(entry, "->")
		if !found {
			continue
		}
		colon := strings.LastIndex(hostSide, ":")
		if colon < 0 {
			// No host IP at all means podman published on every interface.
			return true
		}
		ip := net.ParseIP(strings.Trim(hostSide[:colon], "[]"))
		if ip == nil || !ip.IsLoopback() {
			return true
		}
	}
	return false
}

// RebindInstalledQuadlets reapplies the bind policy to every installed servlo
// container, preserving each unit's image, ports, volumes and custom settings.
// It returns the units that need restarting: those whose file it rewrote, plus
// those whose running container still publishes on the wrong side of the
// policy. That second group is what makes the operation heal itself — a
// container left publicly bound by an install that failed part way is picked
// up on the next run even though its file already reads correctly.
func RebindInstalledQuadlets() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(config.QuadletDir(), "servlo-*.container"))
	if err != nil {
		return nil, err
	}

	restart := make([]string, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".container")
		updated := PairIPv6Binds(BindQuadletPorts(name, string(content)))
		if string(content) != updated {
			config.GuardRealWrite(path)
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return nil, fmt.Errorf("rewriting %s: %w", filepath.Base(path), err)
			}
			restart = append(restart, name)
			continue
		}
		if lanBound, known := ContainerPublishesPubliclyFn(name); known && lanBound != quadletWantsPublicBind(updated) {
			restart = append(restart, name)
		}
	}
	return restart, nil
}

// quadletWantsPublicBind reports whether the given quadlet content publishes beyond
// loopback, i.e. what the running container should look like.
func quadletWantsPublicBind(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "PublishPort=") {
			continue
		}
		value := strings.TrimPrefix(trimmed, "PublishPort=")
		colon := strings.Index(value, ":")
		if colon < 0 {
			continue
		}
		ip := net.ParseIP(strings.Trim(value[:colon], "[]"))
		if strings.HasPrefix(value, "[") {
			if end := strings.Index(value, "]"); end > 0 {
				ip = net.ParseIP(value[1:end])
			}
		}
		if ip == nil || !ip.IsLoopback() {
			return true
		}
	}
	return false
}

// ListManagedServiceNames returns the service names (servlo- prefix and .container
// suffix stripped) of every quadlet carrying CustomServiceQuadletMarker. Used by
// ReconcileServices to find orphans without misclassifying site/worker quadlets.
func ListManagedServiceNames() []string {
	entries, err := filepath.Glob(filepath.Join(config.QuadletDir(), "servlo-*.container"))
	if err != nil {
		return nil
	}
	var names []string
	for _, p := range entries {
		data, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(data, []byte(CustomServiceQuadletMarker)) {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(p), ".container")
		names = append(names, strings.TrimPrefix(base, "servlo-"))
	}
	return names
}

// RemoveQuadlet removes a Podman quadlet container unit file.
func RemoveQuadlet(name string) error {
	path := filepath.Join(config.QuadletDir(), name+".container")
	config.GuardRealWrite(path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveContainer removes a stopped Podman container by name, ignoring errors
// if the container does not exist.
func RemoveContainer(name string) {
	_ = execCommand(PodmanBin(), "rm", "-f", name).Run()
}

// UnitLifecycle is the interface for starting, stopping, restarting, and
// querying service units. Nil in normal operation, where the systemctl
// fallback is used; tests set it to observe unit transitions without a
// service manager.
//
//nolint:unused // a test seam, and the nil checks around it are the fallback.
var UnitLifecycle interface {
	Start(name string) error
	Stop(name string) error
	Restart(name string) error
	UnitStatus(name string) (string, error)
	AllUnitStates() map[string]string
}

// DaemonReload runs the equivalent of systemctl --user daemon-reload, through
// systemd DBus where that is reachable and by shelling out to systemctl when it
// is not.
func DaemonReload() error {
	if err := systemd.DBusDaemonReload(); err == nil {
		return nil
	}
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("daemon-reload failed: %w\n%s", err, out)
	}
	return nil
}

// StartUnit starts a service unit. On Linux it first clears any lingering
// failed state from a previous run so that units which hit Restart=
// rate-limit (e.g. workers that raced container readiness in a buggy
// upgrade) recover automatically on the next `servlo start` instead of
// staying stuck in `failed`.
// AfterUnitChange is fired after every successful StartUnit / StopUnit /
// RestartUnit call. servlo-panel wires this at startup to invalidate the
// systemctl unit cache and publish "sites"/"services" events to the
// eventbus so every browser tab updates in real time — regardless of
// whether the mutation came from an HTTP handler, the CLI, the
// server, or the file watcher. Nil by default so unit tests and binaries
// that don't run the UI don't pay the cost.
var AfterUnitChange func(name string)

// UnitOpDebug controls whether unit-lifecycle calls log a one-line caller
// trace. Defaults to off; set SERVLO_UNIT_OP_DEBUG=1 to enable when chasing
// a "who keeps stopping FPM?" cascade. Cheap when off — runtime.Caller is
// only invoked when the flag is set.
var UnitOpDebug = os.Getenv("SERVLO_UNIT_OP_DEBUG") == "1"

func notifyUnitChange(name string) {
	InvalidateUnitStatusCache(name)
	if AfterUnitChange != nil {
		AfterUnitChange(name)
	}
}

func logUnitOp(action, unit string) {
	if !UnitOpDebug {
		return
	}
	caller := unitOpCaller()
	fmt.Fprintf(os.Stderr, "[servlo] unit-op action=%s unit=%s caller=%s\n", action, unit, caller)
}

// unitOpCaller returns the closest frame outside the podman package — that's
// the servlo-internal site that asked for the unit op. Falls back to "?" if
// the stack walk fails.
func unitOpCaller() string {
	pc := make([]uintptr, 16)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		if !strings.Contains(frame.Function, "realrashid/servlo/internal/podman") {
			return fmt.Sprintf("%s (%s:%d)", frame.Function, filepath.Base(frame.File), frame.Line)
		}
		if !more {
			return frame.Function
		}
	}
}

// errNoRealSystemd is what the unit lifecycle returns when a test drives it with
// no stub installed. Falling through to the real user bus would start, stop and
// enable units on the machine running the suite, which it has already done once.
var errNoRealSystemd = errors.New("podman: refusing the real systemd from a test; set podman.UnitLifecycle")

// realSystemdBlocked reports whether this call must not reach the real user bus.
func realSystemdBlocked() bool { return UnitLifecycle == nil && config.UnderTest() }

func StartUnit(name string) error {
	logUnitOp("start", name)
	if realSystemdBlocked() {
		return errNoRealSystemd
	}
	if UnitLifecycle != nil {
		err := UnitLifecycle.Start(name)
		if err == nil {
			notifyUnitChange(name)
		}
		return err
	}
	if err := systemd.DBusStartUnit(name); err != nil {
		return err
	}
	notifyUnitChange(name)
	return nil
}

// StopUnit stops a service unit.
func StopUnit(name string) error {
	logUnitOp("stop", name)
	if realSystemdBlocked() {
		return errNoRealSystemd
	}
	if UnitLifecycle != nil {
		err := UnitLifecycle.Stop(name)
		if err == nil {
			notifyUnitChange(name)
		}
		return err
	}
	if err := systemd.DBusStopUnit(name); err != nil {
		return err
	}
	notifyUnitChange(name)
	return nil
}

// ResetFailedUnit clears a unit's failed / start-rate-limit state so a
// following RestartUnit recovers a crash-looped worker instead of being
// refused. DBusRestartUnit does not reset-failed the way DBusStartUnit does.
// Best-effort.
func ResetFailedUnit(name string) {
	if UnitLifecycle != nil || realSystemdBlocked() {
		return
	}
	_ = systemd.DBusResetFailed(name)
}

// RestartUnit restarts a service unit.
func RestartUnit(name string) error {
	logUnitOp("restart", name)
	if realSystemdBlocked() {
		return errNoRealSystemd
	}
	if UnitLifecycle != nil {
		err := UnitLifecycle.Restart(name)
		if err == nil {
			notifyUnitChange(name)
		}
		return err
	}
	if err := systemd.DBusRestartUnit(name); err != nil {
		return err
	}
	notifyUnitChange(name)
	return nil
}

// mysqlReadyArgs probes servlo-mysql over IPv4 loopback TCP, never the Unix
// socket (its path differs across mysql/mariadb images). Container-internal
// 127.0.0.1 holds on IPv6-only host networks too.
//
// No password here: the credential travels in the exec's environment, both
// because it is generated per install and because an argument is readable out
// of the process list by every other user on this machine.
var mysqlReadyArgs = []string{"mysqladmin", "ping", "-h127.0.0.1", "-P3306", "-uroot", "--silent"}

// mariadbReadyArgs mirrors mysqlReadyArgs but calls mariadb-admin. The
// mariadb:11 image dropped the legacy mysqladmin symlink, so probing it with
// mysqladmin can never succeed and WaitReady would time out on every poll.
// mariadb-admin is present in every mariadb version servlo ships (10.5+).
var mariadbReadyArgs = []string{"mariadb-admin", "ping", "-h127.0.0.1", "-P3306", "-uroot", "--silent"}

// mysqlProbeArgs is a ping inside unit, with the password in the environment.
// A probe that cannot read the password would report a healthy server as
// unready forever, so a failure to resolve it is a failed probe rather than an
// unauthenticated one.
func mysqlProbeArgs(unit string, probe []string) ([]string, bool) {
	c, err := dbconn.ForFamily("mysql")
	if err != nil {
		return nil, false
	}
	args := []string{"exec"}
	for _, pair := range c.ClientEnv() {
		args = append(args, "--env", pair)
	}
	return append(append(args, unit), probe...), true
}

// readyFamily strips known version suffixes ("mariadb-10-11" →
// "mariadb", "mysql-8.0" → "mysql", "postgres-16" → "postgres") so
// WaitReady's family-aware probes apply to versioned preset names too.
// Without this, the auto-rollback path in serviceops only catches
// catastrophic failures for the bare-name presets — versioned ones
// fall through to the systemd-active probe which can report "active"
// for a few seconds even when the container is crashlooping.
// rustfsProbeAddr is the host address WaitReady dials to test rustfs readiness.
// rustfs is probed over TCP from the host rather than via podman exec (the image
// ships no shell), so the probe must follow the port rustfs is actually published
// on. HostPorts() is the registry's effective primary: the PublishedPort override
// the port-ownership guard sets when a host server owns 9000, else the preset
// default seeded into Port. Without this the dial targets a port nothing is on and
// WaitReady burns its full timeout on every php/composer call. Empty (rustfs not
// configured, so not something WaitReady is starting) resolves to nothing to dial.
func rustfsProbeAddr(svc config.ServiceConfig) string {
	hp := svc.HostPorts()
	if len(hp) == 0 {
		return ""
	}
	return fmt.Sprintf("localhost:%d", hp[0])
}

func readyFamily(service string) string {
	for _, fam := range []string{"mariadb", "mysql", "postgres", "redis", "rustfs"} {
		if service == fam || strings.HasPrefix(service, fam+"-") {
			return fam
		}
	}
	return service
}

// WaitReady polls until the named service is ready to accept connections, or
// timeout is reached. Readiness is tested by running a lightweight probe inside
// the container: mysqladmin ping for mysql, mariadb-admin ping for mariadb,
// pg_isready for postgres, redis-cli ping for redis. For other services it
// falls back to waiting until the systemd unit is "active".
func WaitReady(service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	unit := "servlo-" + service
	family := readyFamily(service)

	var probe func() bool
	switch family {
	case "mysql":
		probe = func() bool {
			args, ok := mysqlProbeArgs(unit, mysqlReadyArgs)
			return ok && execCommand(PodmanBin(), args...).Run() == nil
		}
	case "mariadb":
		probe = func() bool {
			args, ok := mysqlProbeArgs(unit, mariadbReadyArgs)
			return ok && execCommand(PodmanBin(), args...).Run() == nil
		}
	case "postgres":
		probe = func() bool {
			cmd := execCommand(PodmanBin(), "exec", unit,
				"pg_isready", "-U", "postgres")
			return cmd.Run() == nil
		}
	case "redis":
		probe = func() bool {
			cmd := execCommand(PodmanBin(), "exec", unit,
				"redis-cli", "ping")
			return cmd.Run() == nil
		}
	case "rustfs":
		addr := rustfsProbeAddr(config.ServiceConfigFor(service))
		probe = func() bool {
			if addr == "" {
				return false
			}
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err != nil {
				return false
			}
			conn.Close()
			return true
		}
	default:
		probe = func() bool {
			status, _ := UnitStatus(unit)
			return status == "active"
		}
	}

	for time.Now().Before(deadline) {
		if probe() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become ready within %s", service, timeout)
}

// unitStatusCache memoises DBusActiveState calls for a short window so
// dashboard snapshot rebuilds don't issue 100+ DBus round-trips per refresh.
// 2 seconds is short enough that a unit toggle in servlo-panel (which runs the
// AfterUnitChange hook anyway) is reflected promptly, while long enough to
// absorb burst rebuilds during systemd state-change storms.
const unitStatusCacheTTL = 2 * time.Second

type unitStatusEntry struct {
	state string
	at    time.Time
}

var (
	unitStatusCacheMu sync.Mutex
	unitStatusCache   = map[string]unitStatusEntry{}
)

// InvalidateUnitStatusCache drops the cached DBus state for name. Called from
// AfterUnitChange so explicit mutations are visible to the next snapshot
// rebuild without waiting for the TTL.
func InvalidateUnitStatusCache(name string) {
	unitStatusCacheMu.Lock()
	delete(unitStatusCache, name)
	unitStatusCacheMu.Unlock()
}

// UnitStatus returns the active state of a service unit.
func UnitStatus(name string) (string, error) {
	if UnitLifecycle != nil {
		return UnitLifecycle.UnitStatus(name)
	}

	unitStatusCacheMu.Lock()
	if entry, ok := unitStatusCache[name]; ok && time.Since(entry.at) < unitStatusCacheTTL {
		state := entry.state
		unitStatusCacheMu.Unlock()
		return state, nil
	}
	unitStatusCacheMu.Unlock()

	state := systemd.DBusActiveState(name)
	if state == "" {
		state = "unknown"
	}

	unitStatusCacheMu.Lock()
	unitStatusCache[name] = unitStatusEntry{state: state, at: time.Now()}
	unitStatusCacheMu.Unlock()

	return state, nil
}
