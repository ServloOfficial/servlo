package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ServloOfficial/servlo/internal/backup"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/fpmpool"
	"github.com/ServloOfficial/servlo/internal/logrotate"
	"github.com/ServloOfficial/servlo/internal/nginx"
	phpPkg "github.com/ServloOfficial/servlo/internal/php"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/serviceops"
	"github.com/ServloOfficial/servlo/internal/services"
	"github.com/ServloOfficial/servlo/internal/shims"
	"github.com/ServloOfficial/servlo/internal/sitecron"
	"github.com/ServloOfficial/servlo/internal/siteops"
	"github.com/ServloOfficial/servlo/internal/staging"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// quadletImage reads the Image= value from an installed quadlet file.
// Returns "" if the file cannot be read or has no Image= line.
func quadletImage(unit string) string {
	path := filepath.Join(config.QuadletDir(), unit+".container")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, "Image="); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// ensureImages checks all images required by units that are about to start and
// builds or pulls any that are missing, using the parallel spinner UI.
func ensureImages() {
	units := append(coreUnits(), installedServiceUnits()...)
	units = append(units, installedCustomContainerUnits()...)
	var jobs []BuildJob
	seen := map[string]bool{}

	for _, unit := range units {
		image := quadletImage(unit)

		// On macOS there are no quadlet files, so quadletImage returns "".
		// Derive the image name from the unit name for PHP-FPM units so that
		// images are rebuilt after a VM reset without requiring manual intervention.
		if image == "" && strings.HasPrefix(unit, "servlo-php") && strings.HasSuffix(unit, "-fpm") {
			short := strings.TrimSuffix(strings.TrimPrefix(unit, "servlo-php"), "-fpm")
			image = "servlo-php" + short + "-fpm:local"
		}

		if image == "" || seen[image] {
			continue
		}
		seen[image] = true

		if podman.RunSilent("image", "exists", image) == nil {
			continue // already present
		}

		img := image
		switch {
		case strings.HasPrefix(img, "servlo-php") && strings.HasSuffix(img, "-fpm:local"):
			// Extract version from image name, e.g. servlo-php84-fpm:local → 8.4
			short := strings.TrimSuffix(strings.TrimPrefix(img, "servlo-php"), "-fpm:local")
			ver := short[:1] + "." + short[1:]
			v := ver
			jobs = append(jobs, BuildJob{
				Label: "PHP " + v,
				Run: func(w io.Writer) error {
					_, err := podman.BuildFPMImageTo(v, false, w)
					return err
				},
			})

		case strings.HasPrefix(img, "localhost/servlo-frankenphp") && strings.HasSuffix(img, ":local"):
			// Build the derived FrankenPHP image, e.g.
			// localhost/servlo-frankenphp84:local → 8.4
			short := strings.TrimSuffix(strings.TrimPrefix(img, "localhost/servlo-frankenphp"), ":local")
			if len(short) < 2 {
				continue // malformed tag with no version digits; skip rather than panic
			}
			v := short[:1] + "." + short[1:]
			jobs = append(jobs, BuildJob{
				Label: "FrankenPHP " + v,
				Run:   func(w io.Writer) error { return podman.BuildFrankenPHPImage(v, false, w) },
			})

		case strings.HasPrefix(img, "servlo-custom-") && strings.HasSuffix(img, ":local"):
			// Rebuild custom container from the site's Containerfile.
			siteName := strings.TrimSuffix(strings.TrimPrefix(img, "servlo-custom-"), ":local")
			sn := siteName
			jobs = append(jobs, BuildJob{
				Label: "Custom: " + sn,
				Run: func(w io.Writer) error {
					site, err := config.FindSite(sn)
					if err != nil {
						return err
					}
					proj, err := config.LoadProjectConfig(site.Path)
					if err != nil {
						return err
					}
					return podman.BuildCustomImageTo(sn, site.Path, proj.Container, w)
				},
			})

		default:
			label := podman.PlatformImage(img)
			jobs = append(jobs, BuildJob{
				Label: "Pulling " + label,
				Run: func(w io.Writer) error {
					args := append(append([]string{"pull"}, podman.PlatformPullArgs(label)...), label)
					cmd := podman.Cmd(args...)
					cmd.Stdout = w
					cmd.Stderr = w
					return cmd.Run()
				},
			})
		}
	}

	if len(jobs) > 0 {
		RunParallel(jobs) //nolint:errcheck
	}
}

// NewStartCmd returns the start command.
func NewStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Servlo (nginx, PHP-FPM, and installed services)",
		RunE:  runStart,
	}
}

// NewStopCmd returns the stop command.
func NewStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Servlo containers (nginx, PHP-FPM, and running services)",
		RunE:  runStop,
	}
}

// NewQuitCmd returns the quit command.
func NewQuitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quit",
		Short: "Stop all Servlo processes and containers (including UI and watcher)",
		RunE:  runQuit,
	}
}

// ensureDefaultPHPInstalled builds the FPM image and writes the unit file for
// the configured default PHP version if it has never been installed. This
// handles the case where the user sets a new default (e.g. 8.5) before running
// `servlo php install`, so `servlo start` transparently installs it.
func ensureDefaultPHPInstalled() {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil || cfg.PHP.DefaultVersion == "" {
		return
	}
	defaultVer := cfg.PHP.DefaultVersion
	installed, _ := phpPkg.ListInstalled()
	for _, v := range installed {
		if v == defaultVer {
			return // already installed
		}
	}
	fmt.Printf("  --> Installing PHP %s (configured default, not yet installed) ...\n", defaultVer)
	if err := podman.BuildFPMImage(defaultVer, false); err != nil {
		fmt.Printf("  WARN: build PHP %s image: %v\n", defaultVer, err)
		return
	}
	if err := podman.WriteFPMQuadlet(defaultVer); err != nil {
		fmt.Printf("  WARN: write PHP %s unit: %v\n", defaultVer, err)
	}
}

// coreUnits returns the container units managed by servlo start/stop.
// Does not include servlo-panel or servlo-watcher — those are added separately in runStart.
// The configured default PHP version is ALWAYS included so the `php`, `composer`,
// and `laravel new` shims have a working FPM container even on a fresh install
// with zero registered sites. Other installed versions are only started when
// at least one site references them; unused versions are left stopped.
func coreUnits() []string {
	cfg, _ := config.LoadGlobal()
	units := []string{"servlo-nginx"}
	active := activePHPVersions()
	if cfg != nil && cfg.PHP.DefaultVersion != "" {
		active[cfg.PHP.DefaultVersion] = true
	}
	versions, _ := phpPkg.ListInstalled()
	for _, v := range versions {
		if !active[v] {
			continue
		}
		short := strings.ReplaceAll(v, ".", "")
		units = append(units, "servlo-php"+short+"-fpm")
	}
	return units
}

// installedCustomContainerUnits returns units for per-project custom containers
// and per-site FrankenPHP containers that have a unit file installed (plist on
// macOS, quadlet on Linux). These are started alongside FPM and services.
func installedCustomContainerUnits() []string {
	var units []string
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	for _, site := range reg.Sites {
		if site.Paused {
			continue
		}
		var unitName string
		switch {
		case site.IsCustomContainer():
			unitName = podman.CustomContainerName(site.Name)
		case site.IsFrankenPHP():
			unitName = podman.FrankenPHPContainerName(site.Name)
		case site.IsCustomFPM():
			unitName = podman.CustomFPMContainerName(site.Name)
		default:
			continue
		}
		// Use the platform-aware check (plist on macOS, .container quadlet on Linux)
		// rather than podman.QuadletInstalled which only checks for .container files
		// and always returns false on macOS where plists are used instead.
		if services.Mgr.ContainerUnitInstalled(unitName) {
			units = append(units, unitName)
		}
	}
	return units
}

// installedServiceUnits returns service units that have a unit file installed
// and have not been manually stopped by the user. Used for servlo start.
func installedServiceUnits() []string {
	var units []string
	for _, svc := range knownServices() {
		if services.Mgr.ContainerUnitInstalled("servlo-"+svc) && !config.ServiceIsPaused(svc) {
			units = append(units, "servlo-"+svc)
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if services.Mgr.ContainerUnitInstalled("servlo-"+svc.Name) && !config.ServiceIsPaused(svc.Name) {
			units = append(units, "servlo-"+svc.Name)
		}
	}
	return units
}

// allInstalledServiceUnits returns all service units that have a unit file
// installed, regardless of paused state. Used for servlo stop.
func allInstalledServiceUnits() []string {
	var units []string
	for _, svc := range knownServices() {
		if services.Mgr.ContainerUnitInstalled("servlo-" + svc) {
			units = append(units, "servlo-"+svc)
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if services.Mgr.ContainerUnitInstalled("servlo-" + svc.Name) {
			units = append(units, "servlo-"+svc.Name)
		}
	}
	return units
}

// PortCheck pairs a host port with a human-readable label and container name.
type PortCheck struct {
	Port      string // host port number
	Label     string // e.g. "nginx HTTP", "mysql"
	Container string // servlo container name
}

// builtinExtraPorts lists secondary host ports for built-in services that are
// hardcoded in the quadlet files but not reflected in config.ServiceConfig.Port.
var builtinExtraPorts = map[string][]string{
	"rustfs": {"9001"},
}

// hostPort extracts the host port from a port mapping string ("host:container").
// If no colon is present the whole string is returned.
func hostPort(mapping string) string {
	if i := strings.Index(mapping, ":"); i >= 0 {
		return mapping[:i]
	}
	return mapping
}

// CollectPortChecks builds the list of ports to verify for the given units.
func CollectPortChecks(units []string) []PortCheck {
	unitSet := make(map[string]bool, len(units))
	for _, u := range units {
		unitSet[u] = true
	}

	var checks []PortCheck

	// Nginx ports (configurable).
	if unitSet["servlo-nginx"] {
		cfg, err := config.LoadGlobal()
		httpPort := 80
		httpsPort := 443
		if err == nil {
			if cfg.Nginx.HTTPPort > 0 {
				httpPort = cfg.Nginx.HTTPPort
			}
			if cfg.Nginx.HTTPSPort > 0 {
				httpsPort = cfg.Nginx.HTTPSPort
			}
		}
		checks = append(checks,
			PortCheck{strconv.Itoa(httpPort), "nginx HTTP", "servlo-nginx"},
			PortCheck{strconv.Itoa(httpsPort), "nginx HTTPS", "servlo-nginx"},
		)
	}

	// Built-in services.
	cfg, _ := config.LoadGlobal()
	for _, svc := range knownServices() {
		if !unitSet["servlo-"+svc] {
			continue
		}
		container := "servlo-" + svc
		if cfg != nil {
			if sc, ok := cfg.Services[svc]; ok {
				// A PublishedPort override moves the primary published port, so
				// check the real bound port rather than the preset default.
				port := sc.Port
				if sc.PublishedPort > 0 {
					port = sc.PublishedPort
				}
				if port > 0 {
					checks = append(checks, PortCheck{strconv.Itoa(port), svc, container})
				}
				for _, ep := range sc.ExtraPorts {
					checks = append(checks, PortCheck{hostPort(ep), svc, container})
				}
			}
		}
		for _, ep := range builtinExtraPorts[svc] {
			checks = append(checks, PortCheck{ep, svc, container})
		}
	}

	// Custom services.
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if !unitSet["servlo-"+svc.Name] {
			continue
		}
		container := "servlo-" + svc.Name
		for _, p := range svc.Ports {
			checks = append(checks, PortCheck{hostPort(p), svc.Name, container})
		}
	}

	return checks
}

// checkPortConflicts warns about ports already in use by non-servlo processes.
func checkPortConflicts(units []string) {
	checks := CollectPortChecks(units)
	if len(checks) == 0 {
		return
	}

	ss := PortListOutput()
	if ss == "" {
		return
	}

	var conflicts []string
	for _, c := range checks {
		if isPortConflict(c, ss, podmanContainerRunning) {
			conflicts = append(conflicts,
				fmt.Sprintf("  WARN: port %s (%s) already in use, may fail to start (check: %s)", c.Port, c.Label, FindListenerCmd(c.Port)))
		}
	}
	if len(conflicts) > 0 {
		fmt.Println("Port conflicts detected:")
		for _, msg := range conflicts {
			fmt.Println(msg)
		}
		fmt.Println()
	}
}

// isPortConflict reports whether a port check is a genuine clash with a foreign
// process. A servlo service that already owns its port is never a conflict: a
// running container owns it directly, and the podman machine's gvproxy owns any
// published port by forwarding it. The func seam keeps this pure and testable.
func isPortConflict(c PortCheck, portList string, containerRunning func(string) bool) bool {
	if containerRunning(c.Container) {
		return false
	}
	if !PortInUseIn(c.Port, portList) {
		return false
	}
	return !portOwnedByMachineProxy(c.Port, portList)
}

// portOwnedByMachineProxy reports whether the listener on the given port is the
// podman machine's gvproxy. On macOS that proxy owns every published host port
// (servlo's containers themselves carry no -p), so a gvproxy-held port is a
// servlo/podman forward into the VM rather than a foreign blocker. On Linux there
// is no gvproxy, so this never matches and the check is a harmless no-op.
func portOwnedByMachineProxy(port, portList string) bool {
	for _, line := range strings.Split(portList, "\n") {
		if strings.HasPrefix(line, "gvproxy") && strings.Contains(line, ":"+port+" ") {
			return true
		}
	}
	return false
}

// podmanContainerRunning adapts podman.ContainerRunning to the bool-only seam
// isPortConflict expects, treating a probe error as "not running".
func podmanContainerRunning(name string) bool {
	running, _ := podman.ContainerRunning(name)
	return running
}

func runStart(_ *cobra.Command, _ []string) error {
	// Clear the intentional-stop marker up front: we're bringing servlo up, so the
	// worker health watcher should resume reporting real drift once units are back.
	_ = config.ClearStopped()

	// Podman orders every rootless quadlet after its network-online wait unit.
	// Where network-online.target never activates (Fedora Silverblue and other
	// atomic images) that unit only ever times out, so each container start,
	// and the boot itself, stalls for 90s. Override it before starting anything.
	if applied, err := servloSystemd.EnsureNoNetworkWaitStall(); err != nil {
		fmt.Printf("  WARN: skip podman network-online wait: %v\n", err)
	} else if applied {
		fmt.Println("  Skipping podman's network-online wait (this host never reaches that target)")
	}

	// Self-heal a podman upgrade before touching the network or starting
	// containers. A major-version or backend change since the last run
	// reshuffles rootless storage/networking and otherwise surfaces as the
	// cryptic "rootless netns" container start failure (#635). No-op unless
	// drift is detected. The heal force-removes the servlo containers; the start
	// sequence below brings them back up, so the returned list is not needed
	// here.
	containerDNS := podman.ContainerDNS()
	_ = healPodmanUpgrade(containerDNS)

	// Ensure the servlo bridge network exists. On macOS the network is stored
	// inside the Podman Machine VM; it may be absent after a fresh machine
	// init or if it was pruned. All service containers use --network servlo so
	// this must succeed before any container is started.
	if err := podman.EnsureNetwork("servlo", containerDNS); err != nil {
		if errors.Is(err, podman.ErrNetworkNeedsMigration) {
			fmt.Println("  WARN: servlo network schema doesn't match host IPv6 support; run 'servlo install' to recreate")
		} else {
			fmt.Printf("  WARN: ensure servlo network: %v\n", err)
		}
	}

	// Restore quadlets and worker units that may be missing after an
	// uninstall/reinstall cycle. Reads .servlo.yaml from each active site.
	restoreSiteInfrastructure()

	// Reconcile custom services against their YAMLs (issue #678): regenerate a
	// missing quadlet, drop an orphan quadlet with no YAML. Data dirs untouched.
	reconcileCustomServices()

	// Arm log rotation. Here rather than only when the setting is changed, so
	// an install that has never touched the setting still gets the timer, and a
	// server whose unit was lost to a reinstall gets it back.
	if err := logrotate.ApplySchedule(logrotate.Enabled(), servloBinaryPath()); err != nil {
		feedback.Warn("arming log rotation: %v", err)
	}
	// The server's own nightly state backup. Every install gets it: a machine
	// backing its sites up nightly while the registry that makes them
	// restorable ages is the gap a rebuild finds.
	if err := backup.ApplyStateSchedule(true, servloBinaryPath()); err != nil {
		feedback.Warn("arming the server's own backup: %v", err)
	}

	// If the configured default PHP version has never been installed (no plist /
	// quadlet / container), install it now so coreUnits() can include it.
	ensureDefaultPHPInstalled()

	// Pre-flight port conflict check.
	units := append(coreUnits(), installedServiceUnits()...)
	checkPortConflicts(units)

	// Build or pull any missing images before starting containers.
	ensureImages()

	// Rewrite nginx.conf so any config changes in new binary versions take effect.
	// Rewritten on every start so the drop-in matches the flag even after a
	// restore, a rebuild, or a config edited by hand. Best effort: a start must
	// not fail because a php.ini could not be written.
	if cfg, cfgErr := config.LoadGlobal(); cfgErr == nil {
		_ = config.WriteProductionIni(cfg.ProductionMode())
	}

	if err := nginx.EnsureNginxConfig(); err != nil {
		fmt.Printf("  WARN: nginx config: %v\n", err)
	}
	// The credentials an install carried before session authentication become
	// an account here, once, so upgrading does not present the first-run setup
	// form on a machine that already had a password.
	if err := AdoptInheritedCredentials(); err != nil {
		fmt.Printf("[WARN] adopting the existing dashboard credentials: %v\n", err)
	}

	// A panel domain set while servlo was down, or a config edited by hand,
	// takes effect here rather than waiting for the next `panel domain set`.
	if err := ApplyPanelDomain(); err != nil {
		fmt.Printf("[WARN] writing the panel vhost: %v\n", err)
	}
	if err := nginx.EnsureServloVhost(); err != nil {
		fmt.Printf("  WARN: servlo vhost: %v\n", err)
	}
	// The servlo-nginx quadlet bind-mounts RunDir so the servlo.localhost vhost
	// can reach servlo-panel over a unix socket. The directory must exist before
	// the container starts or podman will create it root-owned.
	if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
		fmt.Printf("  WARN: run dir: %v\n", err)
	}

	// Write the shared hosts file mounted into PHP containers at /etc/hosts.
	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("  WARN: container hosts file: %v\n", err)
	}

	// Pre-flight: drop bind mounts whose host directory has gone (a branch
	// checkout that removed it, a deleted project). Podman refuses to start a
	// container with a missing bind source, so one such path otherwise takes
	// nginx and every site down with it (#1083).
	for _, r := range podman.RepairMissingMounts() {
		if r.Site != "" {
			fmt.Printf("  WARN: %s no longer exists (site %s), removed from %s\n", r.Path, r.Site, r.Unit)
		} else {
			fmt.Printf("  WARN: %s no longer exists, removed from %s\n", r.Path, r.Unit)
		}
	}

	// Pre-flight: repair SSL vhosts with missing cert files so nginx can start.
	if repairs := nginx.RepairVhosts(); len(repairs) > 0 {
		for _, r := range repairs {
			switch r.Reason {
			case "missing-cert":
				fmt.Printf("  WARN: missing TLS certificate for %s — switched to HTTP\n", r.Domain)
			case "orphan-ssl":
				fmt.Printf("  WARN: removed orphan SSL vhost for %s\n", r.Domain)
			}
		}
	}

	// Reload nginx if it is already running so regenerated base vhosts (the
	// dashboard and profiler vhosts) take effect without a full restart.
	if running, _ := podman.ContainerRunning("servlo-nginx"); running {
		_ = nginx.Reload()
	}

	// Phase 1: start all infrastructure (containers, FPM, custom containers,
	// UI, watcher) before workers. Workers exec into containers, so they must
	// be up first.
	serviceUnits := append(coreUnits(), installedServiceUnits()...)
	serviceUnits = append(serviceUnits, installedCustomContainerUnits()...)
	serviceUnits = append(serviceUnits, "servlo-panel", "servlo-watcher")

	// Phase 2: worker units that depend on running containers.
	workerUnits := append(registeredQueueUnits(), registeredScheduleUnits()...)
	workerUnits = append(workerUnits, registeredReverbUnits()...)
	// Also include non-standard framework workers (horizon, vite-dev, etc.)
	// declared in the site registry, so restored unit files get started here
	// rather than waiting for the next session.
	workerUnits = append(workerUnits, registeredFrameworkWorkerUnits()...)
	workerUnits = append(workerUnits, registeredTimerUnits()...)
	workerUnits = collapseTimerSiblings(dedupeStrings(workerUnits))
	// Don't resurrect workers the idle engine has gracefully suspended. Without
	// this, a boot or a manual start after stop would start a deliberately-asleep
	// worker while the registry still records it suspended, drifting the dashboard
	// (site shown asleep, workers actually running) and making workerheal skip it.
	// Mirrors the autostart filter; real activity wakes it via the engine.

	feedback.Begin()
	feedback.Line("starting servlo")

	makeJobs := func(us []string) []BuildJob {
		jobs := make([]BuildJob, len(us))
		for i, u := range us {
			unit := u
			label := strings.TrimSuffix(strings.TrimPrefix(unit, "servlo-"), ".timer")
			jobs[i] = BuildJob{
				Label: label,
				Run:   func(w io.Writer) error { return podman.StartUnit(unit) },
			}
		}
		return jobs
	}

	RunParallel(makeJobs(serviceUnits)) //nolint:errcheck
	// Bulk start does not go through servlo service start, so discover_family
	// consumers (phpMyAdmin, pgAdmin) never got a post-engine regen. Reconcile
	// may also have written empty host lists before any engine was up. Refresh
	// once engines are running so PMA_HOSTS / SERVLO_POSTGRES_HOSTS match reality.
	serviceops.RefreshDiscoverFamilyConsumers()
	if len(workerUnits) > 0 {
		RunParallel(makeJobs(workerUnits)) //nolint:errcheck
	}

	// Regenerate the shared-hosts file now that nginx has its IP. The file
	// was written earlier with a possibly stale address; update it so
	// containers on the servlo network resolve site domains to the current
	// servlo-nginx container IP.
	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("  WARN: browser hosts file: %v\n", err)
	}

	// Sync the pasta DNS proxy (169.254.1.1) as the aardvark-dns upstream for the
	// servlo network, so a container resolving a public name reaches the host's
	// resolver rather than an address that means nothing inside the netns.
	if err := podman.EnsureNetworkDNS("servlo", podman.ContainerDNS()); err != nil {
		fmt.Printf("  WARN: network DNS: %v\n", err)
	}

	autoStopUnusedFPMs()

	return nil
}

// startRestoredServices pulls images and starts service units that have a quadlet
// installed but are not yet running. Called from servlo install to bring back services
// (mysql, redis, etc.) that were restored from .servlo.yaml.
func startRestoredServices() {
	units := installedServiceUnits()
	if len(units) == 0 {
		return
	}

	// Pull missing images first.
	var pullJobs []BuildJob
	seen := map[string]bool{}
	for _, unit := range units {
		// PlatformImage covers a quadlet still on the upstream image from before
		// the rewrite landed (idempotent once the unit is rewritten on start).
		image := podman.PlatformImage(quadletImage(unit))
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		if podman.RunSilent("image", "exists", image) == nil {
			continue
		}
		img := image
		pullJobs = append(pullJobs, BuildJob{
			Label: "Pulling " + img,
			Run: func(w io.Writer) error {
				args := append(append([]string{"pull"}, podman.PlatformPullArgs(img)...), img)
				cmd := podman.Cmd(args...)
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		})
	}
	if len(pullJobs) > 0 {
		RunParallel(pullJobs) //nolint:errcheck
	}

	// Start the services.
	var startJobs []BuildJob
	for _, u := range units {
		unit := u
		label := strings.TrimSuffix(strings.TrimPrefix(unit, "servlo-"), ".timer")
		startJobs = append(startJobs, BuildJob{
			Label: label,
			Run:   func(_ io.Writer) error { return podman.StartUnit(unit) },
		})
	}
	feedback.Header("Starting services")
	RunParallel(startJobs) //nolint:errcheck
	// Same discover_family refresh as runStart: engines and admin UIs come up
	// together here without going through StartService.
	serviceops.RefreshDiscoverFamilyConsumers()

	// Workers exec into the FPM containers and depend on servlo-redis et al.
	// Start them after the service containers are up — same ordering as
	// runStart's phase 1 → phase 2 split. Without this, `servlo install` would
	// leave workers enabled-but-stopped after restoreSiteInfrastructure, since
	// restoreWorker only writes the unit file and defers Start to here.
	workerUnits := append(registeredQueueUnits(), registeredScheduleUnits()...)
	workerUnits = append(workerUnits, registeredReverbUnits()...)
	workerUnits = append(workerUnits, registeredFrameworkWorkerUnits()...)
	workerUnits = append(workerUnits, registeredTimerUnits()...)
	workerUnits = collapseTimerSiblings(dedupeStrings(workerUnits))
	// Don't resurrect workers the idle engine has gracefully suspended, exactly
	// as runStart does. Without this, `servlo install`/`update` (which re-creates
	// and re-enables every worker via restoreSiteInfrastructure) restarts a
	// deliberately-asleep worker on an idle site and wedges the engine: the
	// registry still records it suspended, so the dashboard shows the site asleep
	// while its workers run and the engine never re-suspends them.
	if len(workerUnits) == 0 {
		return
	}
	var workerJobs []BuildJob
	for _, u := range workerUnits {
		unit := u
		label := strings.TrimSuffix(strings.TrimPrefix(unit, "servlo-"), ".timer")
		workerJobs = append(workerJobs, BuildJob{
			Label: label,
			Run:   func(_ io.Writer) error { return podman.StartUnit(unit) },
		})
	}
	feedback.Header("Starting workers")
	RunParallel(workerJobs) //nolint:errcheck
}

// reconcileCustomServices heals custom-service drift on start (issue #678).
// Failures are non-fatal so one bad service can't block the start sequence.
func reconcileCustomServices() {
	res, err := serviceops.ReconcileServices(nil)
	if err != nil {
		feedback.Warn("reconciling services: %v", err)
	}
	// A refreshed definition may add client tools (client_shims), so bring the
	// shim dir in line non-interactively.
	if len(res.DefinitionsRefreshed) > 0 {
		_ = shims.Reconcile(nil)
	}
	for _, name := range res.ConfigsApplied {
		feedback.Warn("applied an updated config to %s and restarted it", name)
	}
	// Default-stack services (mysql, postgres, redis…) don't flow through the custom
	// reconcile above, so apply the same config-drift restart to them: a shipped
	// preset config bump (e.g. a higher max_allowed_packet) must reach a running
	// default service on update, not only on an explicit reinstall.
	for _, name := range config.DefaultPresetNames() {
		if applied, err := serviceops.RestartIfConfigDrifted(name, name); err != nil {
			feedback.Warn("applying config for %s: %v", name, err)
		} else if applied {
			feedback.Warn("applied an updated config to %s and restarted it", name)
		}
	}
	for _, name := range res.QuadletsRegenerated {
		feedback.Warn("regenerated missing unit for %s from its config", name)
	}
	for _, name := range res.OrphansRemoved {
		feedback.Warn("removed orphan service %s (unit with no config; data left intact)", name)
	}
	for _, name := range res.RunningOrphansSkipped {
		feedback.Warn("orphan service %s has no config but its container is running; left as-is (remove with: servlo service remove %s)", name, name)
	}
}

// restoreSiteInfrastructure ensures FPM quadlets, service quadlets, and worker
// units exist for all registered (non-paused) sites. This repairs state after
// an uninstall/reinstall cycle where unit files were deleted but site configs
// (sites.yaml, .servlo.yaml) were preserved.
// siteServingFilesMissing reports whether the generated files that make a site
// answerable are absent: its nginx vhost, or the PHP-FPM pool the vhost sends
// requests to.
//
// Both are generated rather than backed up, so a site restored onto a fresh
// server arrives with its registry entry and its files and neither of these.
// `servlo restore` writes only files and the database, and the start path
// regenerated quadlets, services and workers while leaving these two out, so
// every restored site answered with nginx's not-found page and the FPM unit
// failed outright — a pool directory with no pool in it is a config error
// php-fpm exits on rather than a container that comes up idle.
//
// Only a site the shared FPM serves has a pool here. A FrankenPHP, custom
// container or host-proxy site has none, and expecting one would regenerate its
// vhost on every start.
func siteServingFilesMissing(s config.Site) bool {
	if _, err := os.Stat(filepath.Join(config.NginxConfD(), s.PrimaryDomain()+".conf")); err != nil {
		return true
	}
	if s.IsFrankenPHP() || s.IsCustomContainer() || s.IsHostProxy() {
		return false
	}
	poolDir := config.FPMPoolDir(podman.FPMContainerName(s, s.PHPVersion))
	_, err := os.Stat(fpmpool.Path(poolDir, s.Name))
	return err != nil
}

// restoreSiteTimers puts one site's scheduled work back on the machine.
//
// This is the half of a rebuild that stays quiet when it is missing. A restored
// server serves every site correctly and silently stops doing anything on a
// schedule: the backup that was the reason for keeping backups, its test
// restore, and every command the site had cron running. Nothing says so,
// because a timer that was never written cannot fail. The timers are not in a
// backup either, since they live in the user's systemd directory and the state
// archive holds servlo's config.
//
// Both restorers are idempotent and both decide the whole state from the
// registry, so a schedule an operator switched off loses its units here rather
// than coming back from the dead.
func restoreSiteTimers(s config.Site) {
	if err := sitecron.Sync(s); err != nil {
		feedback.Warn("restoring scheduled commands for %s: %v", s.Name, err)
	}
	// Only for a site that has one. ApplySchedule decides all three states, and
	// the state it decides for a site with no schedule is "take the units
	// away", which on an ordinary start is two daemon reloads and four
	// systemctl calls per site to remove units that were never there. A
	// schedule switched off while servlo was not running is cleaned up by the
	// path that switched it off.
	if s.Backup == nil || s.Backup.Schedule == "" {
		return
	}
	if err := backup.ApplySchedule(s, servloBinaryPath()); err != nil {
		feedback.Warn("restoring the backup schedule for %s: %v", s.Name, err)
	}
}

// restoreStagingCredentials puts back the file nginx checks a staging site's
// password against.
//
// A staging site is behind a password and that is not optional, so its vhost
// names an auth_basic_user_file. The file lives under the data directory, which
// the server-state archive does not carry: it holds servlo's config plus the
// registry, and everything else there is a cache, a certificate that will be
// reissued, or an archive. So a rebuild restored the site, regenerated a vhost
// naming an auth file, and wrote no file. nginx answers 500 to every request
// against an auth_basic_user_file it cannot open, which leaves the staging site
// broken rather than closed, on a machine whose operator has just proved their
// backups work.
//
// The hash to write was in the registry the whole time. It was stored by three
// callers and read by none.
//
// Only when the file is missing. Once it exists it is what nginx is already
// checking, and rewriting it every start would let a stale registry entry undo
// a password set since.
func restoreStagingCredentials(s config.Site) {
	if s.Staging == nil || s.Staging.User == "" || s.Staging.Hash == "" {
		return
	}
	domain := s.PrimaryDomain()
	if _, err := os.Stat(nginx.HtpasswdPath(domain)); err == nil {
		return
	}
	if err := staging.WriteHtpasswd(domain, s.Staging.User, s.Staging.Hash); err != nil {
		feedback.Warn("restoring the staging password for %s: %v", s.Name, err)
	}
}

func restoreSiteInfrastructure() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}

	seenPHP := map[string]bool{}
	seenSvc := map[string]bool{}

	// Backfill framework for all sites (including paused) that were linked
	// before detection was added. Detection reads each site's directory, which
	// is far too long to hold the registry lock over, so the answers are
	// collected here and applied at the end in one short locked pass.
	detected := map[string]string{}
	for i, s := range reg.Sites {
		if s.Ignored || s.Framework != "" {
			continue
		}
		if name, ok := config.DetectFrameworkForDir(s.Path); ok {
			reg.Sites[i].Framework = name
			detected[s.Name] = name
		}
	}

	for _, s := range reg.Sites {
		if s.Paused || s.Ignored {
			continue
		}

		// Restore custom container plist/quadlet for custom container sites.
		// On macOS the plist lives in ~/Library/LaunchAgents; on Linux it is a
		// systemd quadlet. After a reinstall the unit file may be gone even though
		// the site is still registered in sites.yaml and .servlo.yaml is on disk.
		if s.IsCustomContainer() {
			unitName := podman.CustomContainerName(s.Name)
			if !services.Mgr.ContainerUnitInstalled(unitName) {
				proj, _ := config.LoadProjectConfig(s.Path)
				if proj != nil && proj.Container != nil {
					if err := podman.WriteCustomContainerQuadlet(s.Name, s.Path, s.ContainerPort); err != nil {
						feedback.Warn("restoring custom container unit for %s: %v", s.Name, err)
					}
				}
			}
		}

		// Restore the per-site quadlet (and image, if missing) for custom-FPM
		// PHP sites, so they come back up on `servlo start` after a reinstall.
		if s.IsCustomFPM() {
			unitName := podman.CustomFPMContainerName(s.Name)
			if !services.Mgr.ContainerUnitInstalled(unitName) {
				proj, _ := config.LoadProjectConfig(s.Path)
				if proj != nil && proj.Container != nil {
					if !podman.CustomImageExists(s.Name) {
						_ = podman.BuildCustomImage(s.Name, s.Path, proj.Container)
					}
					if err := podman.WriteCustomFPMQuadlet(s.Name, s.PHPVersion); err != nil {
						feedback.Warn("restoring custom FPM unit for %s: %v", s.Name, err)
					}
				}
			}
		}

		// Put back the vhost and the pool when either has gone missing. This is
		// the state a restored site arrives in, and the state an operator lands
		// in after removing a conf.d file by hand. Regenerating is safe: both
		// files are derived from the registry and the project config, and a
		// custom nginx override lives in its own file that this does not touch.
		if siteServingFilesMissing(s) {
			site := s
			if err := siteops.RegenerateSiteVhost(&site, site.PrimaryDomain()); err != nil {
				feedback.Warn("restoring the vhost and PHP-FPM pool for %s: %v", site.Name, err)
			}
		}

		restoreSiteTimers(s)
		restoreStagingCredentials(s)

		// Restore FPM quadlet for this site's PHP version (shared-FPM PHP sites
		// only; custom-FPM sites use their per-site container handled above).
		if !s.IsCustomContainer() && !s.IsHostProxy() && !s.IsCustomFPM() {
			phpVer := s.PHPVersion
			if phpVer == "" {
				cfg, _ := config.LoadGlobal()
				phpVer = cfg.PHP.DefaultVersion
			}
			if phpVer != "" && !seenPHP[phpVer] {
				seenPHP[phpVer] = true
				ensureFPMQuadlet(phpVer) //nolint:errcheck
			}
		}

		// Read .servlo.yaml for service and worker info.
		proj, _ := config.LoadProjectConfig(s.Path)
		if proj == nil {
			continue
		}

		// Restore the host-proxy dev-server worker unit. Phase 2 of runStart
		// launches it (it is enumerated by registeredFrameworkWorkerUnits).
		// Bind to the command the user approved at link time: if .servlo.yaml's
		// dev command drifted since (e.g. a git pull), don't silently run the
		// new one, warn and wait for a re-link to re-approve it.
		if s.IsHostProxy() && proj.Proxy != nil {
			if s.HostCommand != "" && proj.Proxy.Command != s.HostCommand {
				feedback.Warn("%s: dev command in .servlo.yaml changed since link; not auto-starting. Run `servlo link` to review and approve.", s.Name)
			} else if w, ok := hostProxyWorker(proj.Proxy); ok && !services.Mgr.IsEnabled(hostProxyWorkerUnit(s.Name)) {
				restoreWorker(s.Name, s.Path, "", hostProxyWorkerName, w)
			}
		}

		// Resolve() returns the rendered CustomService for inline + preset
		// references (e.g. mariadb-11) and (nil, nil) for built-ins. Without
		// it, preset references slipped through to the built-in template path.
		for _, svc := range proj.Services {
			if seenSvc[svc.Name] {
				continue
			}
			seenSvc[svc.Name] = true
			cs, err := svc.Resolve()
			if err != nil {
				feedback.Warn("resolving service %q for %s: %v", svc.Name, s.Name, err)
				continue
			}
			if cs != nil {
				ensureCustomServiceQuadlet(cs) //nolint:errcheck
			} else {
				ensureServiceQuadlet(svc.Name) //nolint:errcheck
			}
		}

		// Restore worker units from saved worker names. The platform helper
		// decides whether to start immediately (Linux) or just write the unit
		// file and let phase 2 of runStart launch it (macOS).
		for _, w := range proj.Workers {
			unitName := "servlo-" + w + "-" + s.Name
			parentEnabled := services.Mgr.IsEnabled(unitName)
			phpVersion := s.PHPVersion
			if phpVersion == "" {
				cfg, _ := config.LoadGlobal()
				phpVersion = cfg.PHP.DefaultVersion
			}
			fwName := s.Framework
			fw, fwOK := config.GetFrameworkForDir(fwName, s.Path)
			if !fwOK || fw.Workers == nil {
				continue
			}
			wDef, ok := fw.Workers[w]
			if !ok {
				continue
			}
			// Skip restore entirely when the platform can't run this worker
			// shape — writeWorkerUnitFile would print a WARN and return
			// (false, nil) every boot.
			if ok, _ := workerSupportedOnPlatform(wDef); !ok {
				continue
			}
			if !parentEnabled {
				restoreWorker(s.Name, s.Path, phpVersion, w, wDef)
			}
		}
	}
	// Re-read under the lock and set only the field this sweep decided, rather
	// than saving a snapshot taken before every site was walked: anything the
	// panel wrote in between is still in the file that way.
	if len(detected) > 0 {
		config.UpdateSites(func(cur *config.SiteRegistry) (bool, error) { //nolint:errcheck
			changed := false
			for i := range cur.Sites {
				if name, ok := detected[cur.Sites[i].Name]; ok && cur.Sites[i].Framework == "" {
					cur.Sites[i].Framework = name
					changed = true
				}
			}
			return changed, nil
		})
	}

	// Restore unit files for standalone custom services (installed globally via
	// `servlo service add`) whose config exists in ~/.config/servlo/services/ but
	// whose unit file (plist on macOS, quadlet on Linux) is missing — e.g. after
	// a reinstall that wiped ~/Library/LaunchAgents or ~/.config/containers/systemd/.
	if customs, err := config.ListCustomServices(); err == nil {
		for _, svc := range customs {
			if !services.Mgr.ContainerUnitInstalled("servlo-" + svc.Name) {
				ensureCustomServiceQuadlet(svc) //nolint:errcheck
			}
		}
	}

	cleanOrphanTimerUnits()

	podman.DaemonReloadFn() //nolint:errcheck
}

// cleanOrphanTimerUnits removes servlo-*.timer files whose sibling .service
// is missing — they can't fire and break parallel start with exit 1.
func cleanOrphanTimerUnits() {
	dir := config.SystemdUserDir()
	entries, _ := filepath.Glob(filepath.Join(dir, "servlo-*.timer"))
	for _, e := range entries {
		base := strings.TrimSuffix(filepath.Base(e), ".timer")
		if _, err := os.Stat(filepath.Join(dir, base+".service")); err == nil {
			continue
		}
		_ = services.Mgr.RemoveTimerUnit(base)
	}
}

// registeredQueueUnits returns unit names for all servlo-queue-* service units
// (i.e. started via `servlo queue:start`).
func registeredQueueUnits() []string {
	return services.Mgr.ListServiceUnits("servlo-queue-*")
}

// registeredScheduleUnits returns unit names for all servlo-schedule-* service units.
func registeredScheduleUnits() []string {
	return services.Mgr.ListServiceUnits("servlo-schedule-*")
}

// registeredReverbUnits returns unit names for all servlo-reverb-* service units.
func registeredReverbUnits() []string {
	return services.Mgr.ListServiceUnits("servlo-reverb-*")
}

// registeredTimerUnits returns names for every servlo-* timer unit on disk,
// each with the explicit `.timer` suffix so callers pass them straight to
// systemctl. These drive scheduled (cron-style) framework workers like
// Laravel <=10's `php artisan schedule:run`.
func registeredTimerUnits() []string {
	return services.Mgr.ListTimerUnits("servlo-*")
}

// registeredFrameworkWorkerUnits returns servlo-{worker}-{site} unit names for
// every site/worker pair declared in the site registry. Used to make sure
// non-standard workers (horizon, vite-dev, etc.) get started in phase 2 of
// runStart, not just the queue/schedule/reverb glob.
func registeredFrameworkWorkerUnits() []string {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}
	out := make([]string, 0)
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		proj, err := config.LoadProjectConfig(s.Path)
		if err != nil || proj == nil {
			continue
		}
		for _, w := range proj.Workers {
			out = append(out, "servlo-"+w+"-"+s.Name)
		}
		// Enumerate the dev-server unit unconditionally: this list also drives
		// stop/quit, so a drifted unit must stay visible to be stoppable. The
		// drift guard lives in restoreSiteInfrastructure, which won't write the
		// drifted command, so start can only ever launch the approved one.
		if s.IsHostProxy() && proj.Proxy != nil && proj.Proxy.Command != "" {
			out = append(out, hostProxyWorkerUnit(s.Name))
		}
	}
	return out
}

// collapseTimerSiblings drops a worker's bare .service entry when its
// .timer sibling is also in the list — the timer is what drives the
// oneshot, the bare .service would just fire schedule:run a second time.
func collapseTimerSiblings(in []string) []string {
	hasTimer := map[string]bool{}
	for _, u := range in {
		if strings.HasSuffix(u, ".timer") {
			hasTimer[strings.TrimSuffix(u, ".timer")] = true
		}
	}
	out := make([]string, 0, len(in))
	for _, u := range in {
		if !strings.HasSuffix(u, ".timer") && hasTimer[u] {
			continue
		}
		out = append(out, u)
	}
	return out
}

// mergeMigrationRestarts unions the containers torn down by the podman-upgrade
// heal with those recreated by a network migration, de-duplicated and order
// preserved, so a run that triggers BOTH restarts every affected container
// exactly once. Overwriting one list with the other left heal-torn-down services
// stopped after install.
func mergeMigrationRestarts(healed, recreated []string) []string {
	return dedupeStrings(append(append([]string(nil), healed...), recreated...))
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// RunStart starts all servlo services (exported for use by the UI server).
func RunStart() error { return runStart(nil, nil) }

// RunStop stops servlo containers (exported for use by the UI server).
func RunStop() error { return runStop(nil, nil) }

// RunQuit stops all servlo processes and containers (exported for use by the UI server).
func RunQuit() error { return runQuit(nil, nil) }

// stopUnitSet returns every unit `servlo stop` tears down.
func stopUnitSet() []string {
	units := append(coreUnits(), allInstalledServiceUnits()...)
	units = append(units, installedCustomContainerUnits()...)
	units = append(units, registeredQueueUnits()...)
	units = append(units, registeredScheduleUnits()...)
	units = append(units, registeredReverbUnits()...)
	units = append(units, registeredFrameworkWorkerUnits()...)
	// Stop scheduled-worker timers explicitly. Stopping the sibling
	// oneshot .service is a no-op (it isn't running between firings),
	// so without this the timer keeps dispatching after `servlo stop`.
	units = append(units, registeredTimerUnits()...)
	return units
}

func runStop(_ *cobra.Command, _ []string) error {
	units := stopUnitSet()

	feedback.Begin()
	feedback.Line("stopping servlo")

	// Mark the intentional shutdown before tearing anything down, so the worker
	// health watcher (which keeps running) suppresses heal/notification noise for
	// the workers we're about to stop. They stay enabled and come back on start.
	_ = config.MarkStopped()

	jobs := make([]BuildJob, len(units))
	for i, u := range units {
		unit := u
		label := strings.TrimSuffix(strings.TrimPrefix(unit, "servlo-"), ".timer")
		jobs[i] = BuildJob{
			Label: label,
			Run:   func(w io.Writer) error { return podman.StopUnit(unit) },
		}
	}
	RunParallel(jobs) //nolint:errcheck
	return nil
}

// quitProcessUnits is the ordered set of host process units `servlo quit` tears
// down after runStop.
//
// `servlo stop` used to leave one unit up as install-level plumbing and quit
// existed to take it down too. That unit belonged to the DNS stack S2.1
// deleted, so the two commands now stop the same set; quit remains as the name
// for tearing the panel and the watcher down with everything else.
func quitProcessUnits() []string {
	return []string{"servlo-panel", "servlo-watcher"}
}

func runQuit(_ *cobra.Command, _ []string) error {
	// Stop containers and services (same as stop), then the host process units.
	if err := runStop(nil, nil); err != nil {
		return err
	}

	for _, unit := range quitProcessUnits() {
		s := feedback.Start("stopping " + unit)
		if err := podman.StopUnit(unit); err != nil {
			s.Fail(err)
		} else {
			s.OK("")
		}
	}

	return nil
}

// canPromptForPassword reports whether sudo would have someone to ask. sudo reads
// the password from the controlling terminal, not from stdin, so /dev/tty is the
// signal: `servlo start < /dev/null` in a terminal can still prompt, and a systemd
// service with neither cannot. term.IsTerminal on stdin alone gets both wrong.
func canPromptForPassword() bool {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return true
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	tty.Close()
	return true
}
