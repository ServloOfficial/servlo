package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"bytes"
	"net/http"

	"github.com/ServloOfficial/servlo/internal/cleanup"
	"github.com/ServloOfficial/servlo/internal/cli"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/daemon"
	"github.com/ServloOfficial/servlo/internal/eventbus"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/monitor"
	"github.com/ServloOfficial/servlo/internal/nginx"
	nodeDet "github.com/ServloOfficial/servlo/internal/node"
	phpDet "github.com/ServloOfficial/servlo/internal/php"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/siteops"
	"github.com/ServloOfficial/servlo/internal/store"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
	"github.com/ServloOfficial/servlo/internal/ui"
	"github.com/ServloOfficial/servlo/internal/version"
	"github.com/ServloOfficial/servlo/internal/watcher"
	"github.com/spf13/cobra"
)

// notifyServloUI posts to the servlo-panel loopback notifier so any unit lifecycle
// change from a CLI process propagates to the dashboard in real time. It
// runs synchronously with a tight timeout: the CLI process is about to
// exit, so a background goroutine would be killed before the POST hits the
// socket. 500ms is more than enough for a loopback round-trip and is barely
// perceptible. If servlo-panel isn't running the POST fails fast and the CLI
// command still succeeds.
func notifyServloUI(_ string) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:7073/api/internal/notify", bytes.NewReader(nil))
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func main() {
	// Cross-process bridge from CLI unit mutations to the running servlo-panel.
	// ui.Start reassigns this in its own process for a direct in-process
	// publish; in the CLI processes we HTTP-POST to the dashboard.
	podman.AfterUnitChange = notifyServloUI

	// A PHP image (re)build orphans the previous image. Flag it here and reclaim
	// once when the command finishes, so a multi-version install/update coalesces
	// into a single safe-tier sweep instead of one per built version.
	var imageRebuilt atomic.Bool
	podman.OnImageRebuilt = func() { imageRebuilt.Store(true) }

	root := &cobra.Command{
		Use:     "servlo",
		Short:   "Servlo — Podman-powered PHP server panel for Ubuntu 24.04 LTS",
		Version: version.String(),
		// Errors are printed once below: a command that already surfaced its
		// failure through the feedback UI (a red ✗ line) is suppressed here so
		// cobra doesn't reprint the same message as a second "Error: …" line.
		SilenceErrors: true,
		// Cobra validates flags and args before PersistentPreRunE runs, so by the
		// time we get here the invocation is well-formed and any failure is a
		// runtime error, not misuse. Silencing usage now keeps the clean ✗ line
		// for runtime errors while still printing the usage block for a wrong
		// flag or arg count. No subcommand defines its own persistent hook, so
		// this runs for every command.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return nil
		},
		// After any command that rebuilt a PHP image, reclaim the now-orphaned
		// image (safe tier, gated by auto_cleanup). Runs once per command.
		PersistentPostRun: func(_ *cobra.Command, _ []string) {
			if imageRebuilt.Load() {
				cleanup.SweepSafe()
			}
		},
	}

	// Register all subcommands
	root.AddCommand(cli.NewInstallCmd())
	root.AddCommand(cli.NewBootstrapCmd())
	root.AddCommand(cli.NewStartCmd())
	root.AddCommand(cli.NewStopCmd())
	root.AddCommand(cli.NewQuitCmd())
	root.AddCommand(cli.NewUpdateCmd(version.Version))
	root.AddCommand(cli.NewToolsUpdateCmd())
	root.AddCommand(cli.NewUninstallCmd())
	root.AddCommand(cli.NewCleanupCmd())
	root.AddCommand(cli.NewParkCmd())
	root.AddCommand(cli.NewInitCmd())
	root.AddCommand(cli.NewLinkCmd())
	root.AddCommand(cli.NewUnlinkCmd())
	root.AddCommand(cli.NewRestartCmd())
	root.AddCommand(cli.NewRebuildCmd())
	root.AddCommand(cli.NewUnparkCmd())
	root.AddCommand(cli.NewSitesCmd())
	root.AddCommand(cli.NewSecureCmd())
	root.AddCommand(cli.NewDNSProviderCmd())
	root.AddCommand(cli.NewProductionCmd())
	root.AddCommand(cli.NewUnsecureCmd())
	root.AddCommand(cli.NewUseCmd())
	root.AddCommand(cli.NewIsolateCmd())
	root.AddCommand(cli.NewIsolateNodeCmd())
	root.AddCommand(cli.NewRuntimeCmd())
	root.AddCommand(cli.NewNodeInstallCmd())
	root.AddCommand(cli.NewNodeUninstallCmd())
	root.AddCommand(cli.NewNodeUseCmd())
	root.AddCommand(cli.NewNodeManageCmd())
	root.AddCommand(cli.NewNodeUnmanageCmd())
	root.AddCommand(cli.NewNodeManagerCmd())
	root.AddCommand(cli.NewJSRuntimeCmd())
	root.AddCommand(cli.NewPhpListCmd())
	root.AddCommand(cli.NewPhpRebuildCmd())
	root.AddCommand(cli.NewPhpCmd())
	root.AddCommand(cli.NewConsoleCmd())
	root.AddCommand(cli.NewTestCmd())
	root.AddCommand(cli.NewVendorBinCmd())
	root.AddCommand(cli.NewNginxCmd())
	root.AddCommand(cli.NewEnvCmd())
	root.AddCommand(cli.NewEnvRestoreCmd())
	root.AddCommand(cli.NewEnvOverrideCmd())
	root.AddCommand(cli.NewEnvCheckCmd())
	root.AddCommand(cli.NewNodeCmd())
	root.AddCommand(cli.NewNpmCmd())
	root.AddCommand(cli.NewNpxCmd())
	root.AddCommand(cli.NewComposerCmd())
	root.AddCommand(cli.NewAuthCmd())
	root.AddCommand(cli.NewSFTPCmd())
	root.AddCommand(cli.NewServiceCmd())
	root.AddCommand(cli.NewStatusCmd())
	root.AddCommand(cli.NewTuiCmd())
	root.AddCommand(cli.NewWhichCmd())
	root.AddCommand(cli.NewCheckCmd())
	root.AddCommand(cli.NewRunCmd())
	root.AddCommand(cli.NewAboutCmd())
	root.AddCommand(cli.NewWhatsnewCmd())
	root.AddCommand(cli.NewManCmd())
	root.AddCommand(cli.NewDoctorCmd())
	root.AddCommand(cli.NewSiteDoctorCmd())
	root.AddCommand(cli.NewBugReportCmd())
	root.AddCommand(cli.NewLogsCmd())
	root.AddCommand(cli.NewQueueCmd())
	root.AddCommand(cli.NewQueueStartCmd())
	root.AddCommand(cli.NewQueueStopCmd())
	root.AddCommand(cli.NewScheduleCmd())
	root.AddCommand(cli.NewScheduleStartCmd())
	root.AddCommand(cli.NewScheduleStopCmd())
	root.AddCommand(cli.NewReverbCmd())
	root.AddCommand(cli.NewReverbStartCmd())
	root.AddCommand(cli.NewReverbStopCmd())
	root.AddCommand(cli.NewHorizonCmd())
	root.AddCommand(cli.NewHorizonStartCmd())
	root.AddCommand(cli.NewHorizonStopCmd())
	root.AddCommand(cli.NewHorizonReloadCmd())
	root.AddCommand(cli.NewOctaneCmd())
	root.AddCommand(cli.NewOctaneReloadCmd())
	root.AddCommand(cli.NewAutostartCmd())
	root.AddCommand(cli.NewFetchCmd())
	root.AddCommand(cli.NewDbCmd())
	root.AddCommand(cli.NewDbImportCmd())
	root.AddCommand(cli.NewDbExportCmd())
	root.AddCommand(cli.NewDbCreateCmd())
	root.AddCommand(cli.NewDbShellCmd())
	root.AddCommand(cli.NewDbSnapshotCmd())
	root.AddCommand(cli.NewDbSnapshotsCmd())
	root.AddCommand(cli.NewDbRestoreCmd())
	root.AddCommand(cli.NewDbSnapshotRmCmd())
	root.AddCommand(cli.NewDbMoveCmd())
	root.AddCommand(cli.NewDbExtensionCmd())
	root.AddCommand(cli.NewDbConnectionCmd())
	root.AddCommand(cli.NewClientExecCmd())
	root.AddCommand(cli.NewShimsCmd())
	root.AddCommand(cli.NewPathEnableCmd())
	root.AddCommand(cli.NewPathDisableCmd())
	root.AddCommand(cli.NewNotifyCmd())
	root.AddCommand(cli.NewPhpExtCmd())
	root.AddCommand(cli.NewPhpBunCmd())
	root.AddCommand(cli.NewPhpPkgCmd())
	root.AddCommand(cli.NewPhpPortsCmd())
	root.AddCommand(cli.NewPhpIniCmd())
	root.AddCommand(cli.NewDomainCmd())
	root.AddCommand(cli.NewGroupCmd())
	root.AddCommand(cli.NewWorkspaceCmd())
	root.AddCommand(cli.NewFrameworkCmd())
	root.AddCommand(cli.NewWorkerCmd())
	root.AddCommand(cli.NewNewCmd())
	root.AddCommand(cli.NewSetupCmd())
	root.AddCommand(cli.NewMinioMigrateCmd())
	root.AddCommand(cli.NewImportCmd())
	root.AddCommand(cli.NewSailCmd())
	root.AddCommand(cli.NewPauseCmd())
	root.AddCommand(cli.NewBackupCmd())
	root.AddCommand(cli.NewRestoreCmd())
	root.AddCommand(cli.NewHardenCmd())
	root.AddCommand(cli.NewAlertsCmd())
	root.AddCommand(cli.NewStagingCmd())
	root.AddCommand(cli.NewAppsCmd())
	root.AddCommand(cli.NewUnpauseCmd())
	root.AddCommand(cli.NewPanelCmd())
	root.AddCommand(cli.NewUsersCmd())
	root.AddCommand(cli.NewSessionsCmd())
	root.AddCommand(cli.NewAuditCmd())
	root.AddCommand(cli.NewRemoteControlCmd())
	root.AddCommand(cli.NewRemoteControlOnCmd())
	root.AddCommand(cli.NewRemoteControlOffCmd())
	root.AddCommand(cli.NewRemoteControlStatusCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newServeUICmd())

	maybeDispatchVendorBin(root)

	if err := root.Execute(); err != nil {
		// Only print errors the command didn't already show the user via a
		// feedback Fail line, so the same failure never appears twice.
		if !feedback.AlreadyShown(err) {
			feedback.Fail(err)
		}
		os.Exit(1)
	}
}

// maybeDispatchVendorBin rewrites os.Args to invoke the hidden `vendor-bin`
// subcommand when the first positional arg doesn't match any registered cobra
// command but does match an executable in the project's `vendor/bin` directory.
// Real servlo commands always win — this only kicks in for unknown names.
func maybeDispatchVendorBin(root *cobra.Command) {
	if len(os.Args) < 2 {
		return
	}
	first := os.Args[1]
	if first == "" || strings.HasPrefix(first, "-") {
		return
	}
	if isKnownCommand(root, first) {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	if !cli.VendorBinExists(cwd, first) {
		return
	}
	rest := os.Args[2:]
	newArgs := make([]string, 0, len(rest)+3)
	newArgs = append(newArgs, os.Args[0], "vendor-bin", first)
	newArgs = append(newArgs, rest...)
	os.Args = newArgs
}

func isKnownCommand(root *cobra.Command, name string) bool {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return true
		}
		for _, a := range c.Aliases {
			if a == name {
				return true
			}
		}
	}
	return false
}

// newServeUICmd returns the serve-ui command.
func newServeUICmd() *cobra.Command {
	return &cobra.Command{
		Use:    "serve-ui",
		Short:  "Start the Servlo UI dashboard server",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			daemon.TuneRuntime()
			return ui.Start(version.Version)
		},
	}
}

// newWatchCmd returns the watch command (used by the watcher systemd service).
func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "watch",
		Short:  "Watch parked directories for new projects (daemon)",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			daemon.TuneRuntime()
			if os.Getenv("SERVLO_DEBUG") != "" {
				watcher.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})))
			}

			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			fmt.Println("Servlo watcher started, monitoring:", cfg.ParkedDirectories)

			// Ensure the catch-all default vhost is always present.
			if err := nginx.EnsureDefaultVhost(); err != nil {
				fmt.Printf("[WARN] default vhost: %v\n", err)
			}

			// Periodically catch deletions that happen while the watcher is busy.
			go func() {
				for range time.Tick(30 * time.Second) {
					if removeStale(cfg) {
						if err := nginx.Reload(); err != nil {
							fmt.Printf("[WARN] nginx reload: %v\n", err)
						}
						// Tell the UI and anyone else subscribed that the
						// sites list changed. Without this the browser keeps
						// showing the deleted site until a manual refresh.
						eventbus.Default.Publish(eventbus.KindSites)
					}
				}
			}()

			// A worker unit whose checkout has gone restart-loops forever, so
			// reconcile on a slow cadence rather than only at boot.
			go func() {
				for range time.Tick(60 * time.Second) {
					if n := cli.PruneOrphanedWorkers(); n > 0 {
						fmt.Printf("[INFO] pruned %d orphaned worker unit(s) whose checkout was removed\n", n)
					}
				}
			}()

			// Watch host gateway reachability. A laptop that changes networks
			// (home wifi → coffee shop → mobile hotspot) ends up with a stale
			// LAN IP for host.containers.internal in the shared /etc/hosts,
			// and host-gateway lookups silently fail until the next servlo start. The
			// watcher verifies the current entry every tick and reprobes
			// only when it stops responding. On a change, host-proxy vhosts
			// (which bake the gateway IP into proxy_pass on Linux) are
			// regenerated so they don't point at the old, now-dead address.
			watcher.OnGatewayIPChange = cli.RegenerateHostProxyVhostsOnGatewayChange
			go watcher.WatchHostGateway(30*time.Second, nil)

			// Self-heal exec-mode framework workers on macOS. Container mode
			// uses podman --restart=always; exec mode runs guard scripts
			// under systemd that can be left orphaned by an interrupted
			// migration or sleep/wake bridge churn. No-op on Linux.
			go watcher.WatchExecWorkers(60 * time.Second)

			// Re-apply the reload watcher's poll cadence when the machine is
			// plugged in or unplugged. The interval is baked into a worker's
			// unit and read once at watcher startup, so an unplugged laptop
			// would otherwise keep polling at the mains rate. No-op where the
			// reload watcher doesn't have to poll.
			go watcher.WatchPower(30 * time.Second)

			// The three failures nothing reports at the moment they happen: a
			// site that stopped answering, a worker that quietly went down, a
			// disk filling up. Each has to be looked for, so look on a timer
			// and keep the alert list in step both ways.
			go monitor.Watch(monitor.Interval)

			// Renew the certificates of secured sites as they age into the
			// reissue window. A Let's Encrypt leaf lasts ninety days and
			// nothing here renewed one, so every secured site on an install
			// went down together three months in.
			go watcher.WatchCertRenewal(watcher.CertRenewalInterval)

			// Reclaim orphaned servlo images (safe tier) on a slow daily cadence,
			// so rebuild leftovers and stale base images don't pile up. Gated by
			// the auto_cleanup config; never touches service images (--deep).
			go watcher.WatchCleanup(time.Hour)

			// Request-timing analytics: the always-on nginx access feed, its rolling
			// aggregate and the durable store behind each site's Request timing view.
			watcher.StartRequestStats()

			// Keep the cached framework store index fresh so offline detection and
			// listing resolve the full catalogue without a network round trip.
			go store.WatchIndex(6 * time.Hour)

			// Watch key site config files and signal queue:restart on change.
			go func() {
				err := watcher.WatchSiteFiles(
					func() []string {
						reg, err := config.LoadSites()
						if err != nil {
							return nil
						}
						paths := make([]string, 0, len(reg.Sites))
						for _, s := range reg.Sites {
							if !s.Ignored {
								paths = append(paths, s.Path)
							}
						}
						return paths
					},
					2*time.Second,
					func(sitePath string) {
						site, err := config.FindSiteByPath(sitePath)
						if err != nil {
							return
						}
						siteChanged := false

						// Custom container and host-proxy sites don't use PHP/Node
						// version detection — skip re-detection to avoid overwriting
						// the empty values with defaults.
						if !site.IsCustomContainer() && !site.IsHostProxy() {
							// Re-detect PHP version in case .servlo.yaml or .php-version changed.
							{
								phpMin, phpMax := "", ""
								if site.Framework != "" {
									if fw, fwOk := config.GetFrameworkForDir(site.Framework, sitePath); fwOk {
										phpMin, phpMax = fw.PHP.Min, fw.PHP.Max
									}
								}
								detected := phpDet.DetectVersionClamped(sitePath, phpMin, phpMax, site.PHPVersion)
								if detected != site.PHPVersion {
									fmt.Printf("PHP version changed for %s: %s -> %s\n", site.Name, site.PHPVersion, detected)
									site.PHPVersion = detected
									siteChanged = true
									if !site.Paused {
										if site.Secured {
											_ = nginx.GenerateSSLVhost(*site, detected)
										} else {
											_ = nginx.GenerateVhost(*site, detected)
										}
										if err := nginx.Reload(); err != nil {
											fmt.Printf("[WARN] nginx reload after php version change for %s: %v\n", site.Name, err)
										}
									}
								}
							}

							// Re-detect Node version in case .servlo.yaml, .node-version, or .nvmrc changed.
							if detected, detErr := nodeDet.DetectVersion(sitePath); detErr == nil && detected != site.NodeVersion {
								fmt.Printf("Node version changed for %s: %s -> %s\n", site.Name, site.NodeVersion, detected)
								site.NodeVersion = detected
								siteChanged = true
							}
						}

						if siteChanged {
							_ = config.AddSite(*site)
						}
						if err := cli.QueueRestartForSite(site.Name, sitePath, site.PHPVersion); err != nil {
							fmt.Printf("[WARN] queue restart for %s: %v\n", site.Name, err)
						}
						eventbus.Default.Publish(eventbus.KindSites)
					},
				)
				if err != nil {
					fmt.Printf("[WARN] site file watcher: %v\n", err)
				}
			}()

			// Goroutines are live; tell systemd we're ready so Type=notify
			// unit starts unblock for any dependent startup sequence
			// (servlo-panel, test harnesses), then reconcile.
			notifyReadyThenScan(servloSystemd.NotifyReady, func() { bootScan(cfg) })

			return watcher.Watch(cfg.ParkedDirectories, func(projectPath string) {
				fmt.Printf("New project detected: %s\n", projectPath)
				registered, err := cli.RegisterProject(projectPath, cfg)
				if err != nil {
					fmt.Printf("[WARN] registering %s: %v\n", projectPath, err)
				} else if registered {
					if err := nginx.Reload(); err != nil {
						fmt.Printf("[WARN] nginx reload: %v\n", err)
					}
					eventbus.Default.Publish(eventbus.KindSites)
				}
			}, func(removedPath string) {
				site, err := config.FindSiteByPath(removedPath)
				if err != nil {
					return // not a registered site
				}
				fmt.Printf("Project deleted: %s (%s)\n", site.Name, removedPath)
				_ = siteops.UnlinkSiteCore(site, nil)
				eventbus.Default.Publish(eventbus.KindSites)
			})
		},
	}
}

// notifyReadyThenScan signals watcher readiness and then runs the boot scan in
// the background. The order matters: the scan can take an unguessable amount of
// time, while `servlo watch` runs under a Type=notify unit with a start
// timeout. Signalling first leaves a slow scan as a slow scan, instead of a
// SIGTERM before readiness and a restart that begins the same work from scratch.
func notifyReadyThenScan(notifyReady, scan func()) {
	notifyReady()
	go scan()
}

// bootScan is the watcher's one-shot reconciliation: register projects parked
// while it was down and drop sites whose directory has gone. Nothing here is a
// precondition for serving, so it runs off the startup path; the periodic
// passes cover whatever it misses.
func bootScan(cfg *config.GlobalConfig) {
	reloadNeeded := false
	for _, dir := range cfg.ParkedDirectories {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			registered, err := cli.RegisterProject(filepath.Join(dir, entry.Name()), cfg)
			if err != nil {
				fmt.Printf("[WARN] %s: %v\n", entry.Name(), err)
			} else if registered {
				reloadNeeded = true
			}
		}
	}

	// Remove stale sites (deleted while we were offline or during the scan above).
	if removeStale(cfg) {
		reloadNeeded = true
	}

	if reloadNeeded {
		if err := nginx.Reload(); err != nil {
			fmt.Printf("[WARN] nginx reload: %v\n", err)
		}
	}
}

// removeStale removes registered sites whose paths no longer exist on disk.
// Covers both parked-dir projects (caught by the fast fsnotify path when it
// fires) and manually site_link'd projects outside any park. Returns true if
// any sites were removed so the caller can reload nginx and publish events.
//
//nolint:unparam // cfg reserved for future per-park gating
func removeStale(_ *config.GlobalConfig) bool {
	reg, err := config.LoadSites()
	if err != nil {
		return false
	}

	removed := false
	for _, site := range reg.Sites {
		if site.Ignored {
			continue
		}
		if _, statErr := os.Stat(site.Path); os.IsNotExist(statErr) {
			fmt.Printf("Removing stale site: %s (%s)\n", site.Name, site.Path)
			s := site
			// Tear down the site's workers and any per-site container before
			// dropping the vhost and registry entry. Without this a host-proxy
			// site's always-restart dev server (and a custom-container/FrankenPHP
			// container) keeps running after the project directory is gone. The
			// nginx reload is batched by the caller.
			if siteops.StopSiteWorkers != nil {
				siteops.StopSiteWorkers(&s)
			}
			if s.IsCustomContainer() {
				_ = podman.StopUnit(podman.CustomContainerName(s.Name))
				podman.RemoveCustomContainer(s.Name)
				_ = podman.RemoveCustomContainerQuadlet(s.Name)
			}
			if s.IsFrankenPHP() {
				_ = podman.StopUnit(podman.FrankenPHPContainerName(s.Name))
				_ = podman.RemoveFrankenPHPQuadlet(s.Name)
			}
			_ = nginx.RemoveVhost(s.PrimaryDomain())
			_ = config.RemoveSite(s.Name)
			_ = config.RemoveSiteFromWorkspaces(s.Name)
			removed = true
		}
	}
	return removed
}
