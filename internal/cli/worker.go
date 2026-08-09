package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
	"github.com/realrashid/servlo/internal/feedback"
	phpDet "github.com/realrashid/servlo/internal/php"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/services"
	"github.com/spf13/cobra"
)

// NewWorkerCmd returns the worker parent command with start/stop/list subcommands.
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage framework-defined workers for the current site",
	}
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())
	cmd.AddCommand(newWorkerListCmd())
	cmd.AddCommand(newWorkerAddCmd())
	cmd.AddCommand(newWorkerRemoveCmd())
	cmd.AddCommand(newWorkerHealCmd())
	return cmd
}

func newWorkerStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a framework worker as a systemd service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			workerName := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, phpVersion, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			worker, ok := fw.Workers[workerName]
			if !ok {
				return fmt.Errorf("framework %q has no worker named %q\nRun 'servlo worker list' to see available workers", fw.Label, workerName)
			}
			if worker.Check != nil && !config.MatchesRule(cwd, *worker.Check) {
				return fmt.Errorf("worker %q requires a dependency that is not installed\nCheck the framework definition for required packages", workerName)
			}
			if err := WorkerStartForSite(site.Name, cwd, phpVersion, workerName, worker, true); err != nil {
				return err
			}
			if !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

func newWorkerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a framework worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			workerName := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			// Allow stopping orphaned workers that have a running unit
			// but are no longer in the framework definition.
			if _, ok := fw.Workers[workerName]; !ok {
				unitName := "servlo-" + workerName + "-" + site.Name
				if !isServiceActiveOrRestarting(unitName) {
					return fmt.Errorf("framework %q has no worker named %q\nRun 'servlo worker list' to see available workers", fw.Label, workerName)
				}
			}
			if err := WorkerStopForSite(site.Name, cwd, workerName); err != nil {
				return err
			}
			if !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

func newWorkerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workers defined for the current site's framework",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			known := make(map[string]bool)
			if len(fw.Workers) == 0 {
				fmt.Printf("Framework %q has no workers defined.\n", fw.Label)
			} else {
				names := make([]string, 0, len(fw.Workers))
				for n, wDef := range fw.Workers {
					if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
						continue
					}
					names = append(names, n)
				}
				sort.Strings(names)
				fmt.Printf("Workers for %s:\n", fw.Label)
				for _, name := range names {
					known[name] = true
					w := fw.Workers[name]
					label := w.Label
					if label == "" {
						label = name
					}
					fmt.Printf("  %-15s %s\n", name, label)
					fmt.Printf("  %-15s command: %s\n", "", w.Command)
				}
			}

			// Detect orphaned workers — running units with no definition.
			orphans := findOrphanedWorkers(site.Name, known)
			if len(orphans) > 0 {
				fmt.Println("\nOrphaned workers (running but not defined):")
				for _, name := range orphans {
					fmt.Printf("  %-15s (stop with: servlo worker stop %s)\n", name, name)
				}
			}
			return nil
		},
	}
}

// resolveSiteAndFramework finds the registered site and its framework for cwd.
// Falls back to framework detection if the site has no Framework set.
// For custom container sites without a framework, a synthetic framework is
// returned that contains only the custom_workers from .servlo.yaml.
// resolveWorkerRestart decides a worker's systemd restart policy.
//
// In production mode it is always "always", whatever the framework store
// declared. on-failure does not respawn a worker that exited cleanly, and a
// queue worker exiting 0 is the ordinary case: it is what a graceful restart, a
// memory limit and Laravel's own --max-jobs all do. On a development machine
// leaving it stopped is a fair reading of "it finished"; on a live one it means
// queued work silently stops being processed and nobody finds out until a
// customer asks where their email went.
func resolveWorkerRestart(declared string) string {
	if cfg, _ := config.LoadGlobal(); cfg.ProductionMode() {
		return "always"
	}
	if declared == "" {
		return "always"
	}
	return declared
}

func resolveSiteAndFramework(cwd string) (*config.Site, *config.Framework, string, error) {
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		site, err = ensureSiteForCwd()
		if err != nil {
			return nil, nil, "", err
		}
	}

	// Custom container sites may not have a framework. Build a synthetic
	// framework from .servlo.yaml custom_workers so the worker commands work.
	if site.IsCustomContainer() && site.Framework == "" {
		fw := &config.Framework{Name: "custom", Label: "custom container"}
		if proj, _ := config.LoadProjectConfig(cwd); proj != nil && len(proj.CustomWorkers) > 0 {
			fw.Workers = proj.CustomWorkers
		}
		return site, fw, "", nil
	}

	fwName := site.Framework
	if fwName == "" {
		return nil, nil, "", fmt.Errorf("site %q has no framework assigned — run 'servlo link' first", site.Name)
	}

	fw, ok := config.GetFrameworkForDir(fwName, cwd)
	if !ok {
		return nil, nil, "", fmt.Errorf("site %q has no framework assigned — run 'servlo link' or 'servlo framework add'", site.Name)
	}

	phpVersion := site.PHPVersion
	if phpVersion == "" {
		phpVersion, err = phpDet.DetectVersion(cwd)
		if err != nil {
			cfg, _ := config.LoadGlobal()
			phpVersion = cfg.PHP.DefaultVersion
		}
	}

	return site, fw, phpVersion, nil
}

// requireFrameworkWorker returns an error if the site's framework doesn't define the named worker.
func requireFrameworkWorker(cwd, workerName string) error {
	_, fw, _, err := resolveSiteAndFramework(cwd)
	if err != nil {
		return err
	}
	if fw.Workers == nil {
		return fmt.Errorf("framework %q has no workers defined", fw.Label)
	}
	if _, ok := fw.Workers[workerName]; !ok {
		return fmt.Errorf("framework %q has no worker named %q\nRun 'servlo worker list' to see available workers", fw.Label, workerName)
	}
	return nil
}

// resolveWorkerCommand returns the command to run for a worker, substituting the
// worker's reload variant (restart on file changes) when the project opted the
// worker into reload mode and the framework declares the variant. The variant
// text comes from the framework definition (FrameworkWorker.ReloadCommand), so
// the store stays the single source of truth and core never rewrites command
// strings.
//
// The polling flag is appended where the watcher can't see host filesystem
// events. On Linux the container shares the host filesystem directly and
// inotify works, so polling is left off to avoid the wasted CPU. See
// watcherNeedsPolling.
//
// The reload command's watcher shells out to node and resolves the chokidar npm
// package from the project's node_modules; when chokidar is missing we keep the
// standard command and tell the user how to enable the watcher rather than
// letting the worker fail to boot. Enabling reload from the CLI or UI refuses
// up front when chokidar is absent (see ApplyHorizonReload), so this fallback
// only bites if the package is removed after the fact.
func resolveWorkerCommand(sitePath, workerName string, w config.FrameworkWorker) string {
	// A worker execs its command straight from its unit, so the framework's
	// cli_ini has to be folded in here too. Magento cannot even bootstrap at
	// PHP's 128M default, so a worker without it simply crash-loops.
	ini := phpIniArgsForDir(sitePath)
	if w.ReloadCommand == "" || !config.ProjectReloadsWorker(sitePath, workerName) {
		return injectPHPIniIntoCommand(w.Command, ini)
	}
	if !projectHasChokidar(sitePath) {
		feedback.Warn("%s auto-reload is on but chokidar is not installed in %s, running the standard command. Install it with: npm install -D chokidar", workerName, sitePath)
		return injectPHPIniIntoCommand(w.Command, ini)
	}
	command := w.ReloadCommand
	if watcherNeedsPolling(sitePath) {
		command += " --poll"
	}
	return injectPHPIniIntoCommand(command, ini)
}

// watcherNeedsPolling reports whether the reload watcher has to poll because
// host filesystem events don't reach it. Delegates to config.WatcherNeedsPolling
// (the canonical predicate, shared with the Octane reload path).
func watcherNeedsPolling(sitePath string) bool {
	return config.WatcherNeedsPolling(sitePath)
}

// workerExecEnvArgs returns the `--env=` flags a worker's `podman exec` needs.
// Currently that is the reload watcher's poll interval, which only applies where
// the watcher has to poll; everywhere else this is empty and the command is
// unchanged. Setting it unconditionally on the polling hosts is harmless for
// workers that run no watcher, since nothing else reads the variable.
func workerExecEnvArgs(sitePath string) []string {
	env := config.WatcherPollEnv(sitePath)
	if env == "" {
		return nil
	}
	return []string{"--env=" + env}
}

// workerExecEnvFlags is workerExecEnvArgs rendered for the unit templates, with
// a leading space so it can be interpolated straight into the command. Mirrors
// workerColorArgs, which splices the same way for the colour flags.
func workerExecEnvFlags(sitePath string) string {
	args := workerExecEnvArgs(sitePath)
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}

// projectHasChokidar reports whether the chokidar package, required by the
// reload command's file watcher, is installed in the project. Delegates to
// config.ProjectHasChokidar.
func projectHasChokidar(sitePath string) bool {
	return config.ProjectHasChokidar(sitePath)
}

// ProjectHasChokidar is the exported view of projectHasChokidar, for the UI
// snapshot to report whether Horizon auto-reload can be enabled for a site.
func ProjectHasChokidar(sitePath string) bool {
	return projectHasChokidar(sitePath)
}

// InstallChokidar runs `npm install --save-dev chokidar` in the project (via
// runNpmCaptured, the shared fnm helper) so Horizon's horizon:listen watcher
// can resolve chokidar (Vite 8 no longer ships it transitively). Output is
// folded into the error on failure so the UI can show why it failed.
func InstallChokidar(sitePath string) error {
	out, err := runNpmCaptured(sitePath, "install", "--save-dev", "chokidar")
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("npm install --save-dev chokidar failed: %s", msg)
	}
	return nil
}

// WorkerStartForSite writes a systemd unit for the given framework worker and starts it.
// The unit name is servlo-{workerName}-{siteName}.
// If the worker has a Proxy config, the proxy port is auto-assigned and the
// nginx vhost is regenerated to include the WebSocket/HTTP proxy block.
// When persist is false the worker is not added to .servlo.yaml, used by the
// auto-start path so vite workers don't appear as user-opted entries.
func WorkerStartForSite(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker, persist bool) error {
	if err := workerStartPreflight(sitePath, workerName, w); err != nil {
		return err
	}

	// Skip lifecycle for worker shapes the current platform can't run.
	// Without this gate macOS hosts proceed past writeWorkerUnitFile (which
	// returns (false, nil) and prints a WARN) into podman.StartUnit on a
	// unit that was never written, surfacing a confusing podman error
	// behind the original WARN.
	if ok, reason := workerSupportedOnPlatform(w); !ok {
		feedback.Warn("worker %s skipped: %s", workerName, reason)
		return nil
	}

	command := resolveWorkerCommand(sitePath, workerName, w)

	// A host worker from an untrusted project .servlo.yaml (custom_workers) runs its
	// command on the host, so require consent before starting it. Consent is keyed
	// on the resolved command actually executed (the reload variant when the
	// project opts in), not w.Command, so a reload_command can't run unshown behind
	// an approved plain command. Trusted workers (store/built-in/overlay) and
	// in-container workers are unaffected.
	if w.Host && w.ProjectOrigin {
		if err := approveHostCommand(siteName, command, fmt.Sprintf("worker %q", workerName)); err != nil {
			return err
		}
	}

	// Stop conflicting workers before starting.
	for _, conflict := range w.ConflictsWith {
		WorkerStopForSite(siteName, sitePath, conflict) //nolint:errcheck
	}

	// Handle proxy port assignment and command augmentation.
	if w.Proxy != nil && w.Proxy.PortEnvKey != "" {
		envPath := filepath.Join(sitePath, ".env")
		port := envfile.ReadKey(envPath, w.Proxy.PortEnvKey)
		if port == "" {
			port = strconv.Itoa(assignWorkerProxyPort(sitePath, w.Proxy.PortEnvKey, w.Proxy.DefaultPort))
			_ = envfile.ApplyUpdates(envPath, map[string]string{w.Proxy.PortEnvKey: port})
		}
		command = command + " --port=" + port
	}

	// A host worker that starts a known dev server is pinned to a port and
	// pointed at a generated config, so it answers on the site's own domain.
	// Anything the mechanism cannot apply cleanly leaves the command as it was.
	if w.Host {
		if tool := config.DevServerToolFor(sitePath, command); tool != nil {
			if args, err := devServerSetup(siteName, sitePath, tool); err != nil {
				feedback.Warn("dev server stays on its own port: %v", err)
			} else if args != "" {
				command += " " + args
			}
		}
	}

	// Workers exec into the container that hosts the site's runtime —
	// custom container, FrankenPHP, or shared FPM. resolveWorkerFPMUnit
	// owns the per-runtime mapping; restoreWorker / writeWorkerExecUnit
	// share the same helper.
	fpmUnit := resolveWorkerFPMUnit(siteName, phpVersion)
	unitName, unitSiteName := workerNames(siteName, sitePath, workerName)

	restart := resolveWorkerRestart(w.Restart)
	label := w.Label
	if label == "" {
		label = workerName
	}

	changed, err := writeWorkerUnitFile(unitName, label, unitSiteName, sitePath, phpVersion, command, restart, w.Schedule, fpmUnit, w.Host)
	if err != nil {
		return fmt.Errorf("writing worker unit: %w", err)
	}

	// Scheduled workers run via a sibling .timer that systemd starts on
	// the configured cadence; the .service is a Type=oneshot triggered by
	// the timer, so we enable and start the .timer rather than the
	// .service. The non-scheduled (daemon) path keeps the original
	// .service-based lifecycle.
	lifecycleTarget := unitName
	if w.Schedule != "" {
		lifecycleTarget = unitName + ".timer"
	}

	if changed {
		// A rewritten unit (e.g. a runtime switch re-pointed the worker at a
		// different container) only takes effect once systemd re-reads it;
		// without this, Enable/Start act on the stale cached unit.
		if err := podman.DaemonReloadFn(); err != nil {
			feedback.Warn("daemon-reload: %v", err)
		}
		if err := services.Mgr.Enable(lifecycleTarget); err != nil {
			feedback.Warn("enable: %v", err)
		}
	}

	// Route through podman.StartUnit/RestartUnit (not services.Mgr directly) so
	// AfterUnitChange fires the dashboard cache invalidate + WS push. On Linux
	// the systemd DBus subscription catches direct services.Mgr calls as a
	// fallback; macOS has no equivalent, so a direct call leaves the UI stale
	// until the next 15s cache poll.
	//
	// When the unit changed and is already active, restart it instead of
	// starting: a start is a no-op on an active unit, so the old process would
	// keep running while every status surface reports the new one. Same trap
	// ensureFPMQuadletTo and rebindHostProxyDevServer already handle.
	startStep := feedback.Start("starting " + label)
	if changed && isServiceActiveOrRestarting(lifecycleTarget) {
		if err := podman.RestartUnit(lifecycleTarget); err != nil {
			startStep.Fail(err)
			return fmt.Errorf("restarting %s worker: %w", workerName, err)
		}
	} else {
		if err := podman.StartUnit(lifecycleTarget); err != nil {
			startStep.Fail(err)
			return fmt.Errorf("starting %s worker: %w", workerName, err)
		}
	}
	startStep.OK("")
	feedback.Note("logs: " + workerLogHint(unitName, w.Host))

	// Regenerate nginx vhost if the worker has proxy config.
	if w.Proxy != nil {
		regenNginxVhost(siteName, sitePath)
	}

	// Persist this worker to .servlo.yaml so servlo install can restore it.
	// Additive: other workers already in the list are not removed. Skipped
	// when persist is false (auto-start path) so auto-started workers don't
	// appear as user-opted entries.
	if persist {
		_ = config.AddProjectWorker(sitePath, workerName)
	}

	return nil
}

func newWorkerAddCmd() *cobra.Command {
	var (
		command       string
		label         string
		restart       string
		checkFile     string
		checkComposer string
		conflictsWith []string
		proxyPath     string
		proxyPortKey  string
		proxyDefPort  int
		global        bool
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a custom worker to this project or global framework overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			site, err := ensureSiteForCwd()
			if err != nil {
				return err
			}

			w := config.FrameworkWorker{
				Label:         label,
				Command:       command,
				Restart:       restart,
				ConflictsWith: conflictsWith,
			}
			if checkFile != "" || checkComposer != "" {
				w.Check = &config.FrameworkRule{File: checkFile, Composer: checkComposer}
			}
			if proxyPath != "" {
				w.Proxy = &config.WorkerProxy{
					Path:        proxyPath,
					PortEnvKey:  proxyPortKey,
					DefaultPort: proxyDefPort,
				}
			}

			action := "added"
			if global {
				fwName := site.Framework
				if fwName == "" {
					return fmt.Errorf("site %q has no framework assigned", site.Name)
				}
				fw := config.LoadUserFramework(fwName)
				if fw == nil {
					fw = &config.Framework{Name: fwName}
				}
				if fw.Workers == nil {
					fw.Workers = make(map[string]config.FrameworkWorker)
				}
				if _, exists := fw.Workers[name]; exists {
					action = "updated"
				}
				fw.Workers[name] = w
				if err := config.SaveFramework(fw); err != nil {
					return fmt.Errorf("saving framework overlay: %w", err)
				}
				fmt.Printf("Custom worker %q %s in global %s overlay\n", name, action, fwName)
			} else {
				if proj, _ := config.LoadProjectConfig(cwd); proj.CustomWorkers != nil {
					if _, exists := proj.CustomWorkers[name]; exists {
						action = "updated"
					}
				}
				if err := config.SetProjectCustomWorker(cwd, name, w); err != nil {
					return fmt.Errorf("saving .servlo.yaml: %w", err)
				}
				fmt.Printf("Custom worker %q %s in .servlo.yaml\n", name, action)
			}
			fmt.Printf("Start it with: servlo worker start %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "Command to run (required)")
	cmd.Flags().StringVar(&label, "label", "", "Human-readable label")
	cmd.Flags().StringVar(&restart, "restart", "", "Restart policy: always or on-failure")
	cmd.Flags().StringVar(&checkFile, "check-file", "", "Only show worker when this file exists")
	cmd.Flags().StringVar(&checkComposer, "check-composer", "", "Only show worker when this Composer package is installed")
	cmd.Flags().StringSliceVar(&conflictsWith, "conflicts-with", nil, "Workers to stop before starting this one")
	cmd.Flags().StringVar(&proxyPath, "proxy-path", "", "URL path to proxy (e.g. /app)")
	cmd.Flags().StringVar(&proxyPortKey, "proxy-port-env-key", "", "Env key holding the worker port")
	cmd.Flags().IntVar(&proxyDefPort, "proxy-default-port", 0, "Default port if env key is missing")
	cmd.Flags().BoolVar(&global, "global", false, "Save to global framework overlay instead of .servlo.yaml")
	_ = cmd.MarkFlagRequired("command")

	return cmd
}

func newWorkerRemoveCmd() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a custom worker from .servlo.yaml or global framework overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			site, err := ensureSiteForCwd()
			if err != nil {
				return err
			}

			// Stop the worker if running, so it can't outlive the
			// definition about to be deleted from .servlo.yaml.
			unit := WorkerUnitName(site.Name, site.Path, name)
			if isServiceActiveOrRestarting(unit) {
				_ = WorkerStopForSite(site.Name, site.Path, name)
			}

			if global {
				fwName := site.Framework
				if fwName == "" {
					return fmt.Errorf("site %q has no framework assigned", site.Name)
				}
				fw := config.LoadUserFramework(fwName)
				if fw == nil || fw.Workers == nil {
					return fmt.Errorf("no global overlay for framework %q", fwName)
				}
				if _, exists := fw.Workers[name]; !exists {
					return fmt.Errorf("worker %q not found in global %s overlay", name, fwName)
				}
				delete(fw.Workers, name)
				if len(fw.Workers) == 0 {
					fw.Workers = nil
				}
				if err := config.SaveFramework(fw); err != nil {
					return fmt.Errorf("saving framework overlay: %w", err)
				}
				fmt.Printf("Custom worker %q removed from global %s overlay\n", name, fwName)
			} else {
				if err := config.RemoveProjectCustomWorker(cwd, name); err != nil {
					return err
				}
				fmt.Printf("Custom worker %q removed from .servlo.yaml\n", name)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Remove from global framework overlay instead of .servlo.yaml")
	return cmd
}

// siteFrameworkName returns the saved framework name for the given site, or "".
// Does not auto-detect — framework should already be set at link time.
func siteFrameworkName(siteName string) string {
	site, err := config.FindSite(siteName)
	if err != nil {
		return ""
	}
	return site.Framework
}

// workerNames returns the systemd unit name and the human-readable display
// site for the given (siteName, sitePath, workerName).
func workerNames(siteName, sitePath, workerName string) (unit, display string) {
	return "servlo-" + workerName + "-" + siteName, siteName
}

// WorkerUnitName is a thin wrapper around workerNames for callers that only
// need the unit name, including callers outside this package, so the naming
// rule is never re-spelled by hand.
func WorkerUnitName(siteName, sitePath, workerName string) string {
	unit, _ := workerNames(siteName, sitePath, workerName)
	return unit
}

// resolveWorkerFPMUnit returns the container name that workers for siteName
// should `podman exec` into. Three cases:
//
//   - custom container site → its own dedicated container
//   - FrankenPHP site       → its dunglas/frankenphp container
//   - everything else       → shared servlo-php<v>-fpm
//
// Centralised here because restoreWorker (linux + darwin) and the macOS
// writeWorker* helpers used to repeat the resolution and missed the
// FrankenPHP branch — workers on FrankenPHP sites ended up exec'ing into
// the shared FPM container that doesn't run their PHP at all.
func resolveWorkerFPMUnit(siteName, phpVersion string) string {
	if site, _ := config.FindSite(siteName); site != nil {
		// Host-proxy sites run their dev server on the host and have no FPM
		// container, so there is nothing to depend on or exec into. The empty
		// name lets writeHostWorkerUnitFile skip the FPM ordering block.
		return podman.SiteContainerName(*site, phpVersion)
	}
	return podman.SharedFPMContainerName(phpVersion)
}

// WorkerStopForSite stops and removes the named worker unit for the given site.
func WorkerStopForSite(siteName, sitePath, workerName string) error {
	unitName, displaySite := workerNames(siteName, sitePath, workerName)
	return stopWorkerUnit(unitName, workerName, displaySite)
}

// stopWorkerUnit tears down a fully-qualified unit name. Disable
// + Stop + RemoveTimerUnit + RemoveServiceUnit are run unconditionally so
// the call works regardless of whether the worker was scheduled (.timer +
// oneshot .service) or a long-running daemon (.service alone). Missing
// units are no-ops at this layer.
func stopWorkerUnit(unitName, label, _ string) error {
	if label == "" {
		label = unitName
	}
	step := feedback.Start("stopping " + label)
	_ = services.Mgr.Disable(unitName + ".timer")
	podman.StopUnit(unitName + ".timer") //nolint:errcheck
	_ = services.Mgr.Disable(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := services.Mgr.RemoveTimerUnit(unitName); err != nil {
		step.Fail(err)
		return fmt.Errorf("removing timer unit file: %w", err)
	}
	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		step.Fail(err)
		return fmt.Errorf("removing unit file: %w", err)
	}
	// Drop the macOS exec-mode guard script + pid file (no-op on Linux).
	// Without this they linger in ~/.local/share/servlo/run/workers after
	// a normal stop and confuse later mode-migration discovery.
	removeWorkerExecArtifacts(unitName)
	finalizeStopStep(step, podman.DaemonReloadFn())
	return nil
}

// finalizeStopStep closes the worker-stop step and only then surfaces a
// non-fatal daemon-reload error, so the warning lands on its own line instead
// of being overwritten by the live spinner's in-place redraw.
func finalizeStopStep(step *feedback.Step, reloadErr error) {
	step.OK("")
	if reloadErr != nil {
		feedback.Warn("daemon-reload: %v", reloadErr)
	}
}

// workerNameForSiteUnit parses a worker unit name shaped servlo-<worker>-<site>
// and returns <worker>. ok is false when the unit is not a worker unit for
// siteName.
func workerNameForSiteUnit(unit, siteName string) (string, bool) {
	rem, ok := strings.CutPrefix(unit, "servlo-")
	if !ok {
		return "", false
	}
	marker := "-" + siteName
	for idx := strings.Index(rem, marker); idx > 0; {
		after := rem[idx+len(marker):]
		if after == "" || strings.HasPrefix(after, "-") {
			return rem[:idx], true
		}
		next := strings.Index(rem[idx+1:], marker)
		if next < 0 {
			break
		}
		idx += 1 + next
	}
	return "", false
}

// siteOwnsWorkerUnit reports whether unit unambiguously belongs to siteName: the
// name must parse as siteName's worker unit AND no other registered site parse
// it too. Worker-unit names are ambiguous (servlo-horizon-web is both a "web"
// site's horizon worker and a "" site's "horizon-web" worker), so when another
// registered site also matches we decline rather than risk tearing down the
// wrong site's unit; the cost is at most leaving one unit behind.
func siteOwnsWorkerUnit(unit, siteName string, others []string) (string, bool) {
	worker, ok := workerNameForSiteUnit(unit, siteName)
	if !ok {
		return "", false
	}
	for _, o := range others {
		if o == siteName {
			continue
		}
		if _, also := workerNameForSiteUnit(unit, o); also {
			return "", false
		}
	}
	return worker, true
}

// stopAllSiteWorkerUnits stops and removes every worker unit for a site by
// listing units rather than walking the checkout, so it works even after the
// site path is deleted (watcher prune). Only units siteOwnsWorkerUnit confirms
// are unambiguously this site's are torn down.
func stopAllSiteWorkerUnits(site *config.Site) {
	var others []string
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Name != "" && s.Name != site.Name {
				others = append(others, s.Name)
			}
		}
	}
	seen := map[string]bool{}
	for _, glob := range []string{"servlo-*-" + site.Name, "servlo-*-" + site.Name + "-*"} {
		for _, unit := range services.Mgr.ListServiceUnits(glob) {
			if seen[unit] {
				continue
			}
			worker, ok := siteOwnsWorkerUnit(unit, site.Name, others)
			if !ok {
				continue
			}
			seen[unit] = true
			_ = stopWorkerUnit(unit, worker, site.Name)
		}
	}
}

// isServiceActiveOrRestarting returns true if the unit is active or activating.
func isServiceActiveOrRestarting(name string) bool {
	status, _ := podman.UnitStatus(name)
	return status == "active" || status == "activating"
}

// approveHostCommand gates running a project-supplied command on the host for a
// site: it proceeds when the global switch allows it or the user already approved
// the exact command, prompts and persists the approval interactively, and refuses
// non-interactively when unapproved. what labels the thing being run for messages
// (e.g. `worker "vite"`). Empty command is a no-op.
func approveHostCommand(siteName, command, what string) error {
	if command == "" {
		return nil
	}
	allowed, disabled := config.HostCommandAllowed(siteName, command)
	if disabled {
		return fmt.Errorf("%s: project-supplied host commands are disabled (set host_commands.disabled: false to allow)", what)
	}
	if allowed {
		return nil
	}
	if !isInteractive() {
		return fmt.Errorf("%s: not approved; run it once interactively to confirm, or set host_commands.skip_confirmation: true", what)
	}
	fmt.Printf("\nservlo will run this on your host, outside any container:\n\n  %s\n", command)
	if !promptConfirm(fmt.Sprintf("Run it for %s?", siteName)) {
		return fmt.Errorf("%s declined for %s", what, siteName)
	}
	return config.ApproveSiteCommand(siteName, command)
}

// findOrphanedWorkers returns worker names that are running but not in the known set.
func findOrphanedWorkers(siteName string, known map[string]bool) []string {
	suffix := "-" + siteName
	prefix := "servlo-"
	units := services.Mgr.ListServiceUnits("servlo-*-" + siteName)
	var sites []config.Site
	if reg, err := config.LoadSites(); err == nil {
		sites = reg.Sites
	}
	// A host-proxy site's dev server (servlo-app-<site>) is the main process, not
	// an orphan; handled here so callers don't each special-case it.
	hostProxySite := false
	for _, s := range sites {
		if s.Name == siteName && s.IsHostProxy() {
			hostProxySite = true
			break
		}
	}
	var orphans []string
	for _, unit := range units {
		workerName := strings.TrimPrefix(unit, prefix)
		workerName = strings.TrimSuffix(workerName, suffix)
		if workerName == "" || known[workerName] {
			continue
		}
		if hostProxySite && workerName == config.HostProxyWorkerName {
			continue
		}
		switch workerName {
		case "php84-fpm", "php83-fpm", "php82-fpm", "php81-fpm", "php80-fpm",
			"nginx", "dns", "dns-forwarder", "watcher", "ui", "stripe":
			continue
		}
		if isServiceActiveOrRestarting(unit) {
			orphans = append(orphans, workerName)
		}
	}
	return orphans
}
