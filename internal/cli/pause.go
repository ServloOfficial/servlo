package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/nginx"
	phpDet "github.com/realrashid/servlo/internal/php"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/sitecron"
	"github.com/realrashid/servlo/internal/siteinfo"
	servloSystemd "github.com/realrashid/servlo/internal/systemd"
	"github.com/spf13/cobra"
)

// NewPauseCmd returns the pause command.
func NewPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause [site]",
		Short: "Pause a site: stop its workers and replace the vhost with a landing page",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := resolveSiteName(args)
			if err != nil {
				return err
			}
			feedback.Begin()
			return PauseSite(name)
		},
	}
}

// NewUnpauseCmd returns the unpause command.
func NewUnpauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "unpause [site]",
		Aliases: []string{"resume"},
		Short:   "Resume a paused site: restore its vhost and restart previously running workers",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := resolveSiteName(args)
			if err != nil {
				return err
			}
			feedback.Begin()
			return UnpauseSite(name)
		},
	}
}

// PauseSite stops all running workers for the site, replaces its nginx vhost with a
// landing page, and marks it paused in the registry.
func PauseSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}
	if site.Paused {
		feedback.Line(name + " is already paused")
		return nil
	}

	running := collectRunningWorkers(site)

	for _, w := range running {
		stopWorkerByName(site, w)
	}

	// Stop the custom container when pausing a custom container site.
	if site.IsCustomContainer() {
		_ = podman.StopUnit(podman.CustomContainerName(site.Name))
	}
	if site.IsFrankenPHP() {
		_ = podman.StopUnit(podman.FrankenPHPContainerName(site.Name))
	}
	if site.IsCustomFPM() {
		_ = podman.StopUnit(podman.CustomFPMContainerName(site.Name))
	}

	// Strip the container's quadlet [Install] so a paused runtime site's
	// container doesn't come back at the next login via the podman generator.
	// StopUnit above only stops the running instance; the generator re-wires an
	// [Install]-bearing quadlet into default.target.wants on every boot.
	if setSiteContainerAutostart(site, false) {
		_ = podman.DaemonReloadFn()
	}

	// Scheduled commands stop with the workers. A timer still running php
	// artisan against a site the operator took offline is the site working with
	// nobody able to reach it, and the entries keep their unit files so the
	// resume below puts back exactly what was scheduled.
	if err := sitecron.Stop(*site); err != nil {
		feedback.Warn("stopping %s's scheduled commands: %v", name, err)
	}

	if err := writePausedHTML(site); err != nil {
		return fmt.Errorf("writing paused page: %w", err)
	}

	if err := nginx.GeneratePausedVhost(*site); err != nil {
		return fmt.Errorf("generating paused vhost: %w", err)
	}

	site.Paused = true
	site.PausedWorkers = running
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating registry: %w", err)
	}

	nginx.ReloadOrWarn("")

	feedback.Start("pausing " + name).OK(feedback.Val(site.PrimaryDomain()))

	autoStopUnusedServices()

	return nil
}

// siteContainerUnit returns the per-site container unit name for a
// runtime-backed site (FrankenPHP, custom container, custom FPM), or "" for a
// plain FPM site that runs in the shared container and has no dedicated unit.
func siteContainerUnit(site *config.Site) string {
	switch {
	case site.IsCustomContainer():
		return podman.CustomContainerName(site.Name)
	case site.IsFrankenPHP():
		return podman.FrankenPHPContainerName(site.Name)
	case site.IsCustomFPM():
		return podman.CustomFPMContainerName(site.Name)
	}
	return ""
}

// setSiteContainerAutostart strips (on=false) or restores (on=true) the
// [Install] section of a site container's quadlet, the same lever `servlo
// autostart` uses globally, so a paused runtime site's container stops
// autostarting at boot. Restoring is gated on the global autostart flag so
// unpause never re-arms a container the user disabled globally. Returns whether
// the file changed. No-op when the site has no container quadlet (plain FPM, or
// the macOS plist path). Caller daemon-reloads when it returns true.
func setSiteContainerAutostart(site *config.Site, on bool) bool {
	unit := siteContainerUnit(site)
	if unit == "" {
		return false
	}
	path := filepath.Join(config.QuadletDir(), unit+".container")
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	out := podman.StripInstallSection(string(raw), true)
	if on && servloSystemd.IsAutostartEnabled() {
		out = strings.TrimRight(out, "\n") + "\n\n" + quadletInstallBlock
	}
	if out == string(raw) {
		return false
	}
	return os.WriteFile(path, []byte(out), 0644) == nil
}

// hostProxyEnvRefresh regenerates a site's .env by re-running `servlo env`. A
// package var so tests can stub the re-exec; production points it at runServloEnv.
var hostProxyEnvRefresh = runServloEnv

// refreshHostProxyEnvOnResume regenerates a host-proxy site's .env as it unpauses,
// so a published port that moved while it was paused (and was skipped by the
// move-time follow) is picked up before its dev server comes back. Container sites
// reach services by name on the unchanged internal port, so this no-ops for them.
// Reports whether a refresh ran.
func refreshHostProxyEnvOnResume(site *config.Site) bool {
	if site == nil || !site.IsHostProxy() {
		return false
	}
	if err := hostProxyEnvRefresh(site.Path); err != nil {
		feedback.Warn("refreshing host-proxy env on resume: %v", err)
	}
	return true
}

// UnpauseSite restores the site's nginx vhost, restarts any workers that were
// running when the site was paused, and clears the paused state.
func UnpauseSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}
	if !site.Paused {
		feedback.Line(name + " is not paused")
		return nil
	}

	phpVersion := site.PHPVersion

	// Re-arm the container's quadlet [Install] that PauseSite stripped, before
	// starting it, so it autostarts at login again. Gated on the global autostart
	// flag inside the helper, so unpause never re-arms a globally-disabled site.
	if setSiteContainerAutostart(site, true) {
		_ = podman.DaemonReloadFn()
	}

	switch {
	case site.IsCustomContainer():
		// Start the custom container and restore the proxy vhost.
		_ = podman.StartUnit(podman.CustomContainerName(site.Name))
		if site.Secured {
			if err := nginx.GenerateCustomSSLVhost(*site); err != nil {
				return fmt.Errorf("generating custom SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateCustomVhost(*site); err != nil {
				return fmt.Errorf("generating custom vhost: %w", err)
			}
		}
	case site.IsHostProxy():
		// No container; the dev-server worker is restarted below from
		// PausedWorkers. Just restore the proxy vhost.
		if site.Secured {
			if err := nginx.GenerateHostProxySSLVhost(*site); err != nil {
				return fmt.Errorf("generating host-proxy SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing host-proxy SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateHostProxyVhost(*site); err != nil {
				return fmt.Errorf("generating host-proxy vhost: %w", err)
			}
		}
	case site.IsFrankenPHP():
		_ = podman.StartUnit(podman.FrankenPHPContainerName(site.Name))
		if site.Secured {
			if err := nginx.GenerateFrankenPHPSSLVhost(*site); err != nil {
				return fmt.Errorf("generating FrankenPHP SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateFrankenPHPVhost(*site); err != nil {
				return fmt.Errorf("generating FrankenPHP vhost: %w", err)
			}
		}
	default:
		if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}

		if site.IsCustomFPM() {
			// Per-site FPM container; the shared servlo-php<ver>-fpm is not used.
			_ = podman.StartUnit(podman.CustomFPMContainerName(site.Name))
		} else if phpVersion != "" {
			if err := ensureFPMQuadlet(phpVersion); err != nil {
				feedback.Warn("ensuring FPM for PHP %s: %v", phpVersion, err)
			}
		}

		if site.Secured {
			if err := nginx.GenerateSSLVhost(*site, phpVersion); err != nil {
				return fmt.Errorf("generating SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateVhost(*site, phpVersion); err != nil {
				return fmt.Errorf("generating vhost: %w", err)
			}
		}
	}

	nginx.ReloadOrWarn("")

	startServicesForSite(site.Path)

	// A port move that landed while this site was paused skipped it (the follow
	// excludes paused sites), so a host-proxy site's .env can still point at a
	// vacated published port. Regenerate before the dev server resumes below.
	refreshHostProxyEnvOnResume(site)

	resumed := site.PausedWorkers
	for _, w := range resumed {
		resumeWorkerByName(site, w, phpVersion)
	}

	if err := sitecron.Resume(*site); err != nil {
		feedback.Warn("restoring %s's scheduled commands: %v", name, err)
	}

	site.Paused = false
	site.PausedWorkers = nil
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating registry: %w", err)
	}

	// The shared paused.html is left in place for other paused sites.

	feedback.Start("resuming " + name).OK(feedback.Val(site.PrimaryDomain()))
	return nil
}

// ensureServicesForCwd starts any services referenced in the site's .env that
// are not already running. When the site is paused it prints a notice; when it
// is active it starts any missing services silently.
func ensureServicesForCwd(cwd string) {
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return
	}
	siteName := ""
	if site.Paused {
		siteName = site.Name
	}
	startServicesForSiteNoticed(cwd, siteName)
}

// startServicesForSite reads the site's .env file and ensures every servlo service
// it references is running. Called when resuming a paused site.
func startServicesForSite(sitePath string) {
	startServicesForSiteNoticed(sitePath, "")
}

// startServicesForSiteNoticed is like startServicesForSite but prints a header
// notice (using siteName) only when at least one service actually needs starting.
// Pass an empty siteName to suppress the header.
func startServicesForSiteNoticed(sitePath, siteName string) {
	envData, err := os.ReadFile(filepath.Join(sitePath, ".env"))
	if err != nil {
		return
	}
	envContent := string(envData)

	candidates := knownServices()
	if customs, cErr := config.ListCustomServices(); cErr == nil {
		for _, c := range customs {
			candidates = append(candidates, c.Name)
		}
	}

	headerPrinted := false
	for _, name := range candidates {
		if !envfile.ReferencesContainer(envContent, name) {
			continue
		}
		if siteName != "" && !headerPrinted && !servloSystemd.IsServiceActive("servlo-"+name) {
			fmt.Printf("[servlo] site %q is paused — starting required services...\n", siteName)
			headerPrinted = true
		}
		if err := ensureServiceRunning(name); err != nil {
			feedback.Warn("could not start %s: %v", name, err)
		}
	}
}

// CollectRunningWorkerNames returns the names of active workers for the site,
// including stripe. Used to sync .servlo.yaml.
func CollectRunningWorkerNames(site *config.Site) []string {
	return collectRunningWorkers(site)
}

// collectRunningWorkers returns the names of all active or restarting workers
// for the site. Uses IsServiceActiveOrRestarting so crash-looping workers are
// also detected and can be stopped on unlink/pause.
func collectRunningWorkers(site *config.Site) []string {
	var active []string

	// Enumerate all workers from the framework definition. Use
	// podman.UnitStatus rather than systemd.IsServiceActiveOrRestarting
	// because the latter is a no-op stub on darwin, which would make
	// every `servlo worker start … && SetProjectWorkers(CollectRunningWorkerNames)`
	// chain wipe the workers list it just appended to.
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok && fw.Workers != nil {
		names := make([]string, 0, len(fw.Workers))
		for wName := range fw.Workers {
			names = append(names, wName)
		}
		sort.Strings(names)
		states := siteinfo.AllUnitStates()
		for _, wName := range names {
			unit := "servlo-" + wName + "-" + site.Name
			// Scheduled workers' .service sits at inactive between timer firings.
			if unitIsActiveOrActivating(unit) ||
				timerIsActive(states, unit) {
				active = append(active, wName)
			}
		}
	}

	// Stripe is not a framework worker — check it separately.
	if unitIsActiveOrActivating("servlo-stripe-" + site.Name) {
		active = append(active, "stripe")
	}

	// Host-proxy sites supervise a single "app" dev-server worker that is
	// neither a framework worker nor in proj.Workers, so the orphan scan below
	// won't see it. Check it explicitly.
	if site.IsHostProxy() && unitIsActiveOrActivating(hostProxyWorkerUnit(site.Name)) {
		active = append(active, hostProxyWorkerName)
	}

	// Detect orphaned workers — running units with no framework definition.
	known := make(map[string]bool, len(active))
	for _, a := range active {
		known[a] = true
	}
	active = append(active, servloSystemd.FindOrphanedWorkers(site.Name, known)...)

	return active
}

// timerIsActive reports whether a worker's sibling .timer is active, answered
// from the batched unit snapshot instead of a per-unit DBus round trip.
//
// systemd.IsTimerActive fetches the unit's entire property dictionary to read
// one field. The idle engine calls this for every worker of every site it
// believes suspended, every tick, which on a box with a dozen suspended sites
// made that pass the daemon's largest remaining source of idle wakeups.
// AllUnitStates answers from the one cached systemctl enumeration the dashboard
// already populates, and it keeps the .timer suffix for exactly this lookup.
func timerIsActive(states map[string]string, unit string) bool {
	return states[unit+".timer"] == "active"
}

// unitIsActiveOrActivating routes through podman.UnitStatus so the check
// works on macOS too (UnitLifecycle is the darwin service manager, which
// reads launchctl). systemd.IsServiceActiveOrRestarting falls through to
// the DBus stub on darwin and always reports "inactive".
func unitIsActiveOrActivating(unit string) bool {
	state, _ := podman.UnitStatus(unit)
	return state == "active" || state == "activating"
}

// stopWorkerByName stops a single named worker for the site.
func stopWorkerByName(site *config.Site, workerName string) {
	if workerName == "stripe" {
		StripeStopForSite(site.Name) //nolint:errcheck
		return
	}
	WorkerStopForSite(site.Name, site.Path, workerName) //nolint:errcheck
}

// resumeWorkerByName restarts a single named worker for the site. It gates on
// workerResumable so a worker can never be paused-but-unresumable (stranded).
// A new resumable worker kind must be taught to workerResumable or this gate
// blocks it.
func resumeWorkerByName(site *config.Site, workerName, phpVersion string) {
	if !workerResumable(site, workerName) {
		return
	}
	if workerName == "stripe" {
		scheme := "http"
		if site.Secured {
			scheme = "https"
		}
		StripeStartForSite(site.Name, site.Path, scheme+"://"+site.PrimaryDomain()) //nolint:errcheck
		return
	}
	if workerName == hostProxyWorkerName {
		if proj, _ := config.LoadProjectConfig(site.Path); proj != nil && proj.Proxy != nil {
			startHostProxyWorker(*site, proj.Proxy)
		}
		return
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.Workers == nil {
		return
	}
	worker, ok := fw.Workers[workerName]
	if !ok {
		return
	}
	WorkerStartForSite(site.Name, site.Path, phpVersion, workerName, worker, true) //nolint:errcheck
}

// pausedPageHTML is the static HTML for the shared paused-site landing page.
// A single file is served for all paused sites; JavaScript reads the hostname
// and calls the correct unpause API endpoint.
const pausedPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Site Paused — Servlo</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; }
    body {
      background: #0f1117;
      color: #e5e7eb;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      margin: 0;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3142;
      border-radius: 14px;
      padding: 2.5rem 3rem;
      max-width: 420px;
      width: calc(100% - 2rem);
      text-align: center;
    }
    .logo {
      width: 48px;
      height: 48px;
      margin: 0 auto 1.25rem;
      background: #FF2D20;
      border-radius: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-weight: 700;
      font-size: 1.2rem;
      color: #fff;
    }
    h1 { font-size: 1.2rem; font-weight: 600; margin: 0 0 0.5rem; }
    .host {
      font-size: 0.85rem;
      color: #FF2D20;
      font-family: ui-monospace, 'Cascadia Code', monospace;
      margin: 0 0 1rem;
      word-break: break-all;
    }
    p {
      font-size: 0.85rem;
      color: #9ca3af;
      margin: 0 0 1.5rem;
      line-height: 1.5;
    }
    .actions { display: flex; gap: 0.5rem; }
    a, button {
      flex: 1;
      display: inline-block;
      text-decoration: none;
      text-align: center;
      border-radius: 8px;
      padding: 0.6rem 0;
      font-size: 0.85rem;
      font-weight: 500;
      cursor: pointer;
      transition: background 0.15s;
      border: none;
    }
    .btn-primary { background: #FF2D20; color: #fff; }
    .btn-primary:hover:not(:disabled) { background: #e02419; }
    .btn-primary:disabled { background: #374151; cursor: not-allowed; color: #9ca3af; }
    .btn-secondary { background: #262a36; color: #e5e7eb; border: 1px solid #2d3142; }
    .btn-secondary:hover { background: #2d3142; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">L</div>
    <h1>Site Paused</h1>
    <p class="host" id="host"></p>
    <p>This site has been paused. Resume it to restore the application and restart any workers.</p>
    <div class="actions">
      <a href="http://servlo.localhost" class="btn-primary">Open the dashboard</a>
    </div>
  </div>
  <script>
    document.getElementById('host').textContent = location.hostname;
  </script>
</body>
</html>
`

// writePausedHTML ensures the shared paused landing page exists.
func writePausedHTML(_ *config.Site) error {
	dir := config.PausedDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "paused.html"), []byte(pausedPageHTML), 0644)
}

// workerResumable reports whether resumeWorkerByName knows how to bring the
// named worker back, so pause never stops one it cannot restart.
func workerResumable(site *config.Site, workerName string) bool {
	switch workerName {
	case "stripe":
		return true
	case hostProxyWorkerName:
		// resumeWorkerByName only restarts the host-proxy worker when the project
		// still declares a proxy command; without this guard a site whose proxy
		// block was removed would be paused but never resumed.
		proj, _ := config.LoadProjectConfig(site.Path)
		return proj != nil && proj.Proxy != nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.Workers == nil {
		return false
	}
	_, ok = fw.Workers[workerName]
	return ok
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
