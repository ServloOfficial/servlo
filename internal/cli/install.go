package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/nginx"
	nodeDet "github.com/ServloOfficial/servlo/internal/node"
	phpDet "github.com/ServloOfficial/servlo/internal/php"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/ports"
	"github.com/ServloOfficial/servlo/internal/serverbasics"
	"github.com/ServloOfficial/servlo/internal/serviceops"
	"github.com/ServloOfficial/servlo/internal/services"
	"github.com/ServloOfficial/servlo/internal/shims"
	"github.com/ServloOfficial/servlo/internal/siteops"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
	"github.com/spf13/cobra"
)

// healPodmanUpgrade runs the podman-upgrade self-heal, renders its progress,
// and returns the containers it tore down so the caller can restart them. Both
// install and start enter the heal through here so the wording and error
// handling stay identical regardless of entry point.
func healPodmanUpgrade(containerDNS []string) []string {
	healed, restart, err := podman.HealPodmanUpgrade(containerDNS, func(msg string) { feedback.Note(msg) })
	if err != nil {
		feedback.Warn("podman upgrade heal: %v", err)
	} else if healed {
		feedback.Note("podman upgrade detected — migrated storage and rebuilt the servlo network")
	}
	return restart
}

// NewInstallCmd returns the install command.
func NewInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Run one-time Servlo setup",
		RunE:  runInstall,
	}
	cmd.Flags().Bool("no-ipv6", false,
		"Force the servlo network to v4-only even if the host supports IPv6 (also: SERVLO_DISABLE_IPV6=1)")
	cmd.Flags().Bool("from-update", false, "")
	_ = cmd.Flags().MarkHidden("from-update")
	cmd.Flags().Bool("unattended", false,
		"Run non-interactively for package installs on Linux: no prompts, and skip the sudo-gated system steps that `servlo bootstrap` handles")
	cmd.Flags().String("database", "",
		"Which database new sites go on: mysql, mariadb, postgres, or none. A managed database is added afterwards with `servlo db:connection add`")
	return cmd
}

// ensureDatabaseService installs and starts the chosen engine, indirected so a
// test can exercise the choice without a container runtime.
var ensureDatabaseService = serviceops.EnsureServiceRunning

// installDatabaseChoice records which database new sites go on.
//
// The engine is picked here rather than discovered later because it is the one
// database decision that is awkward to change once sites exist: moving a site
// between engines is a dump and a reload, not a setting. A managed database is
// deliberately not asked for at install time, since it needs a host and
// credentials that belong in a prompt rather than an installer flag, and adding
// one afterwards puts new sites on it just the same.
func installDatabaseChoice(cmd *cobra.Command) error {
	choice, _ := cmd.Flags().GetString("database")
	choice = strings.ToLower(strings.TrimSpace(choice))
	if choice == "" || choice == "none" {
		return nil
	}

	if dbconn.DialectForService(choice) == "" {
		return fmt.Errorf("--database %s is not a database servlo runs: mysql, mariadb, postgres, or none", choice)
	}

	step("Setting " + choice + " as the database new sites go on")
	if err := ensureDatabaseService(choice); err != nil {
		return fmt.Errorf("installing %s: %w", choice, err)
	}
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	if _, exists := reg.Find(choice); !exists {
		if err := reg.Add(dbconn.LocalConnection(choice, choice)); err != nil {
			return err
		}
	}
	if err := reg.SetDefault(choice); err != nil {
		return err
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		return err
	}
	ok()
	return nil
}

// step/ok render one install action in the shared feedback vocabulary: step
// prints a dim "→ label…" in place (no spinner, so steps that wrap verbose
// subprocess output don't fight an animation), ok finishes the line with a
// green check. There's no trailing newline after step, so on the error path a
// feedback.Warn completes the dangling line as "→ label… ⚠ msg" in place of the
// check; steps that print multi-line output first emit their own newline.
func step(label string) { fmt.Printf(" %s %s ", feedback.Dim("→"), feedback.Dim(label+"…")) }
func ok()               { fmt.Println(feedback.Green("✓")) }

// fileChangedBy runs mutate and reports whether the file at path differs
// before vs after. A read error on either side is treated as empty content,
// so a file that didn't exist before and does after counts as a change. Used
// by the install pass to bounce a unit only when its on-disk config actually
// moved, rather than on every reinstall.
func fileChangedBy(path string, mutate func() error) (bool, error) {
	before, _ := os.ReadFile(path)
	if err := mutate(); err != nil {
		return false, err
	}
	after, _ := os.ReadFile(path)
	return string(after) != string(before), nil
}

// portPreflightConflicts returns the core host ports servlo needs to bind first
// (nginx HTTP/HTTPS and DNS) that are already held by a foreign process.
// portList is the host listener dump from PortListOutput; the seams mirror
// checkPortConflicts, so servlo's own running container is not reported as a
// conflict with itself.
func portPreflightConflicts(portList string, containerRunning func(string) bool) []PortCheck {
	var conflicts []PortCheck
	for _, c := range CollectPortChecks([]string{"servlo-nginx"}) {
		if isPortConflict(c, portList, containerRunning) {
			conflicts = append(conflicts, c)
		}
	}
	return conflicts
}

// ensurePortsAvailable warns, before any setup work runs, when a core port servlo
// needs is already taken by a foreign process. The usual culprit is a parallel
// local-dev stack such as Laravel Herd or a system nginx/Apache holding 80/443.
// It is advisory only: install continues so a user who knows the conflict (or
// plans to remap servlo's ports) isn't blocked.
func ensurePortsAvailable() {
	step("Checking required host ports")
	portList := PortListOutput()
	if portList == "" {
		ok()
		return
	}
	conflicts := portPreflightConflicts(portList, podmanContainerRunning)
	if len(conflicts) == 0 {
		ok()
		return
	}
	fmt.Println()
	for _, c := range conflicts {
		feedback.Warn("port %s (%s) is already in use, %s may fail to start", c.Port, c.Label, c.Container)
		feedback.Note("find it: " + FindListenerCmd(c.Port))
	}
	feedback.Note("another local stack such as Laravel Herd may be hosting sites; stop it to free these ports, then re-run servlo install")
}

func runInstall(cmd *cobra.Command, _ []string) error {
	feedback.Header("Installing Servlo")

	noIPv6, _ := cmd.Flags().GetBool("no-ipv6")
	if !noIPv6 && os.Getenv("SERVLO_DISABLE_IPV6") == "1" {
		noIPv6 = true
	}
	fromUpdate, _ := cmd.Flags().GetBool("from-update")
	unattended, _ := cmd.Flags().GetBool("unattended")
	// Captured before any step writes config: a missing file means this is a
	// first install, the only time the DNS question is asked. Every later run
	// honours the saved choice rather than re-prompting.
	// Unattended runs are driven by a package maintainer script: reuse the
	// non-interactive update path for prompts. The sudo-gated system steps are
	// skipped here because `servlo bootstrap --system` performs them as root
	// beforehand, so the install itself needs no prompts.
	if err := checkUnattendedSupported(unattended); err != nil {
		return err
	}
	if unattended {
		fromUpdate = true
	}
	if noIPv6 {
		podman.MarkIPv6Disabled("servlo")
		feedback.Line("IPv6 disabled by user, servlo network will be v4-only")
		feedback.Note("delete " + podman.IPv6DisabledMarkerPath("servlo") + " and re-run `servlo install` to re-enable")
	}

	ensurePortsAvailable()

	// Skipped under --unattended: this escalates to root, which a package
	// maintainer script cannot answer for. `servlo bootstrap --system` already
	// applied the same steps beforehand.
	if !unattended {
		if err := runSystemSetup(); err != nil {
			return err
		}
	}
	if err := ensurePortForwarding(); err != nil {
		return err
	}

	// 1. Directories
	step("Creating directories")
	dirs := []string{
		config.ConfigDir(), config.DataDir(), config.BinDir(),
		config.NginxDir(), config.NginxConfD(), config.NginxCustomD(),
		certs.SitesDir(),
		config.QuadletDir(), config.SystemdUserDir(),
		config.DataSubDir("mysql"), config.DataSubDir("redis"),
		config.DataSubDir("postgres"), config.DataSubDir("meilisearch"),
		config.DataSubDir("rustfs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	ok()

	// 2. Podman network
	// Containers removed by recreate are restarted AFTER the quadlet refresh
	// phase below so they come up on the freshly written quadlets.
	var migrated []string
	desiredDNS := podman.ContainerDNS()

	// 2a. Self-heal a podman upgrade. A major-version or backend change since
	// the last install reshuffles rootless storage and networking, which
	// otherwise surfaces as the cryptic "rootless netns" container start
	// failure (#635). Runs before the network step since it recreates it; any
	// containers it tears down join the unconditional restart loop below.
	migrated = append(migrated, healPodmanUpgrade(desiredDNS)...)

	// 2b. Podman orders every rootless quadlet after its network-online wait
	// unit, which can only time out on hosts where network-online.target is
	// never pulled in (Fedora Silverblue and other atomic images). Left alone
	// it stalls every container start, and the boot, by 90s.
	if applied, err := servloSystemd.EnsureNoNetworkWaitStall(); err != nil {
		feedback.Note(fmt.Sprintf("could not skip podman's network-online wait: %v", err))
	} else if applied {
		feedback.Note("skipping podman's network-online wait (this host never reaches that target)")
	}

	step("Creating servlo podman network")
	if err := podman.EnsureNetwork("servlo", desiredDNS); err != nil {
		if errors.Is(err, podman.ErrNetworkNeedsMigration) {
			fmt.Println()
			restored, dualStack, mErr := podman.RecreateNetwork("servlo", desiredDNS)
			if mErr != nil {
				return mErr
			}
			if dualStack {
				feedback.Note("recreated servlo network as dual-stack v4+v6")
			} else {
				feedback.Note("recreated servlo network as v4-only (IPv6 not available for containers)")
			}
			feedback.Note("existing containers on this network were recreated")
			// Union, don't overwrite: `migrated` already holds the containers the
			// upgrade heal tore down (line above). Replacing it with `restored`
			// here dropped those from the restart loop, leaving services stopped
			// after a run that triggered both heal and a network migration.
			migrated = mergeMigrationRestarts(migrated, restored)
			step("Creating servlo podman network")
		} else {
			return err
		}
	}
	if err := podman.EnsureNetworkDNS("servlo", desiredDNS); err != nil {
		return err
	}
	ok()

	// Resolve Node management and which version manager to drive BEFORE
	// downloadBinaries, which skips the fnm zip when node.manager is "nvm".
	bunPath := nodeDet.BunPath()
	if bunPath != "" {
		feedback.Line(fmt.Sprintf("bun detected at %s, servlo will use it automatically for projects that use bun", bunPath))
	}

	var savedNode *bool
	savedManager := ""
	savedNvmDir := ""
	if nodeCfg, err := config.LoadGlobal(); err == nil && nodeCfg != nil {
		if v, set := nodeCfg.NodeManagedPref(); set {
			savedNode = &v
		}
		savedManager = nodeCfg.Node.Manager
		savedNvmDir = nodeCfg.NodeNvmDir()
	}
	systemNode := detectSystemNode()
	nvmDetected := detectNvm()
	wantServloNode, promptNode, nodeDefault := nodeManageDecision(fromUpdate, unattended, savedNode, systemNode != "", nvmDetected, servloManagesNode())
	if promptNode {
		if systemNode != "" {
			feedback.Line("Node.js detected at " + systemNode)
		} else {
			feedback.Line("nvm detected at " + nodeDet.DiscoverNvmDir())
		}
		prompt := "Let servlo manage Node.js versions (installs shims, may override system node)?"
		switch {
		case nvmDetected:
			prompt += " Decline to leave Node to your existing nvm."
		case bunPath != "":
			prompt += " Decline to keep your system Node and use bun."
		}
		wantServloNode = confirmInstallPromptDefault(prompt, nodeDefault)
	}

	// Only on the run that actually makes the choice: once persisted, savedManager
	// is set and neither line comes back.
	nodeManager := nodeManagerChoice(savedManager, wantServloNode, nvmDetected)
	if savedManager == "" && nvmDetected {
		if nodeManager == "nvm" {
			feedback.Line("leaving Node to your nvm, servlo will run npm and npx through it")
		} else {
			feedback.Line("using the bundled fnm for servlo-managed Node, switch with: servlo node:manager nvm")
		}
	}

	// Captured before the save below: a flip either way leaves existing host
	// worker units routing through the old manager until they are regenerated.
	nodeStateChanged := nodeStateFlipped(servloManagesNode(), savedManager, wantServloNode, nodeManager)

	nvmDirToSave := savedNvmDir
	if nodeManager == "nvm" {
		if nvmDirToSave == "" {
			nvmDirToSave = nodeDet.DiscoverNvmDir()
		}
	} else {
		nvmDirToSave = ""
	}

	if nodeCfg, err := config.LoadGlobal(); err == nil && nodeCfg != nil {
		changed := false
		if v, set := nodeCfg.NodeManagedPref(); !set || v != wantServloNode {
			nodeCfg.SetNodeManaged(wantServloNode)
			changed = true
		}
		if nodeCfg.Node.Manager != nodeManager {
			nodeCfg.SetNodeManager(nodeManager)
			changed = true
		}
		if nodeCfg.NodeNvmDir() != nvmDirToSave {
			nodeCfg.SetNodeNvmDir(nvmDirToSave)
			changed = true
		}
		if changed {
			if err := config.SaveGlobal(nodeCfg); err != nil {
				fmt.Printf("    WARN: persist Node-management choice: %v\n", err)
			}
		}
	}

	// Server basics. Reported rather than applied: every one needs root, and
	// servlo never runs sudo on its own behalf. The root pass through
	// `servlo bootstrap --system` applies them; this is what an operator who
	// installed without it has to run themselves.
	reportServerBasics()

	// 3. Binaries (composer, fnm) — after manager is persisted so an
	// nvm choice skips the fnm download.
	step("Downloading binaries")
	if err := downloadBinaries(os.Stdout); err != nil {
		return err
	}
	ok()

	// Ask before RunParallel steals stdin. Only offer the Laravel installer
	// when at least one PHP version is already installed — composer needs a
	// PHP runtime, and asking the question on a fresh install (where no
	// servlo-php*-fpm container exists) would just lead to a confusing failure.
	// Skip the prompt entirely when laravel/installer is already present in
	// the user's composer global vendor dir, since re-running install should
	// not pester the user about something that is already set up.
	var wantLaravelInstaller bool
	if installedPHP, _ := phpDet.ListInstalled(); len(installedPHP) > 0 && !laravelInstallerPresent() {
		wantLaravelInstaller = confirmInstallPrompt("Install Laravel installer (laravel new)?")
	}

	// 6. Nginx
	step("Writing nginx configuration")
	if err := nginx.EnsureNginxConfig(); err != nil {
		return err
	}
	if err := nginx.EnsureDefaultVhost(); err != nil {
		return err
	}
	if err := nginx.EnsureServloVhost(); err != nil {
		return err
	}
	// The servlo-nginx quadlet bind-mounts RunDir so the servlo.localhost vhost
	// can reach servlo-panel over a unix socket. Must exist before nginx starts.
	if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
		return err
	}
	ok()

	// Ahead of the regen pass, which reads the registry below: a secondary left
	// on plain HTTP under a secured main has no 443 block and the main's
	// wildcard answers its subdomain. This is the reconcile an install re-runs.
	secured, securedErr := siteops.EnforceGroupSecondaries()
	if len(secured) > 0 {
		feedback.Note("restored https for group secondaries: " + strings.Join(secured, ", "))
	}
	if securedErr != nil {
		feedback.Warn("securing group secondaries: %v", securedErr)
	}

	step("Regenerating vhosts")
	reg, err := config.LoadSites()
	if err == nil {
		cfg, _ := config.LoadGlobal()
		for _, site := range reg.Sites {
			// Skip paused and ignored sites — they have their own vhosts
			// (landing page or none) that should not be overwritten.
			if site.Paused || site.Ignored {
				continue
			}
			switch {
			case site.IsHostProxy():
				if site.Secured {
					if err := nginx.GenerateHostProxySSLVhost(site); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
						continue
					}
					sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
					mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
					os.Remove(mainConf)          //nolint:errcheck
					os.Rename(sslConf, mainConf) //nolint:errcheck
				} else {
					if err := nginx.GenerateHostProxyVhost(site); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
					}
				}
			case site.IsCustomContainer():
				if site.Secured {
					if err := nginx.GenerateCustomSSLVhost(site); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
						continue
					}
					sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
					mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
					os.Remove(mainConf)          //nolint:errcheck
					os.Rename(sslConf, mainConf) //nolint:errcheck
				} else {
					if err := nginx.GenerateCustomVhost(site); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
					}
				}
			case site.IsFrankenPHP():
				if site.Secured {
					if err := nginx.GenerateFrankenPHPSSLVhost(site); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
						continue
					}
					sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
					mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
					os.Remove(mainConf)          //nolint:errcheck
					os.Rename(sslConf, mainConf) //nolint:errcheck
				} else {
					if err := nginx.GenerateFrankenPHPVhost(site); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
					}
				}
			default:
				phpVer := site.PHPVersion
				if phpVer == "" && cfg != nil {
					phpVer = cfg.PHP.DefaultVersion
				}
				if site.Secured {
					if err := nginx.GenerateSSLVhost(site, phpVer); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
						continue
					}
					sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
					mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
					os.Remove(mainConf)          //nolint:errcheck
					os.Rename(sslConf, mainConf) //nolint:errcheck
				} else {
					if err := nginx.GenerateVhost(site, phpVer); err != nil {
						fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
					}
				}
			}
		}
	}
	ok()

	// WriteQuadlet centrally applies the unit-aware bind policy: nginx
	// publishes on every interface, every managed service stays on loopback.
	// WriteQuadletDiff lets this install restart only units whose binds changed.
	// This also repairs drift from older releases without starting inactive
	// services.
	changedQuadlets := []string{}
	extraVolumes := podman.ExtraVolumePaths()
	// rewriteEmbedded handles the remaining embedded-template quadlets:
	// nginx (lives at the network edge, gets extra volumes for sites outside $HOME).
	rewriteEmbedded := func(name string) error {
		content, err := podman.GetQuadletTemplate(name + ".container")
		if err != nil {
			return nil //nolint:nilerr // missing template = nothing to write
		}
		content = podman.InjectExtraVolumes(content, extraVolumes)
		changed, err := podman.WriteQuadletDiff(name, content)
		if err != nil {
			return err
		}
		if changed {
			changedQuadlets = append(changedQuadlets, name)
		}
		return nil
	}
	// rewriteDefaultPreset handles the YAML-driven default services (mysql,
	// postgres, redis, meilisearch, rustfs). The shared serviceops
	// path applies user image / extra-port overrides and the platform image
	// override, so the install pass produces byte-identical output to the
	// runtime service path (no perpetual "PublishPort changed" diff).
	rewriteDefaultPreset := func(svc string) error {
		path := filepath.Join(config.QuadletDir(), "servlo-"+svc+".container")
		before, _ := os.ReadFile(path)
		if err := serviceops.EnsureDefaultPresetQuadlet(svc); err != nil {
			return err
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			changedQuadlets = append(changedQuadlets, "servlo-"+svc)
		}
		return nil
	}

	step("Writing nginx quadlet")
	if err := rewriteEmbedded("servlo-nginx"); err != nil {
		return err
	}
	ok()

	step("Refreshing service quadlets")
	for _, svc := range config.DefaultPresetNames() {
		if !podman.QuadletInstalled("servlo-" + svc) {
			continue
		}
		_ = rewriteDefaultPreset(svc)
	}
	ok()

	if err := installDatabaseChoice(cmd); err != nil {
		return err
	}

	// Always ensure the default PHP-FPM is available (needed for servlo new on fresh installs).
	// Then restore quadlets for any additional PHP versions and services from registered sites.
	{
		cfg, _ := config.LoadGlobal()
		seenPHP := map[string]bool{}
		seenSvc := map[string]bool{}

		if cfg != nil && cfg.PHP.DefaultVersion != "" {
			seenPHP[cfg.PHP.DefaultVersion] = true
			if err := ensureFPMQuadlet(cfg.PHP.DefaultVersion); err != nil {
				fmt.Printf("  WARN: default PHP %s FPM quadlet: %v\n", cfg.PHP.DefaultVersion, err)
			}
		}

		reg, regErr := config.LoadSites()
		if regErr == nil {

			for _, s := range reg.Sites {
				if s.Paused || s.Ignored {
					continue
				}

				// Restore FPM quadlet.
				v := s.PHPVersion
				if v == "" && cfg != nil {
					v = cfg.PHP.DefaultVersion
				}
				if v != "" && !seenPHP[v] {
					seenPHP[v] = true
					if err := ensureFPMQuadlet(v); err != nil {
						fmt.Printf("  WARN: PHP %s FPM quadlet: %v\n", v, err)
					}
				}

				// Restore service quadlets from .servlo.yaml.
				proj, _ := config.LoadProjectConfig(s.Path)
				if proj == nil {
					continue
				}
				for _, svc := range proj.Services {
					if seenSvc[svc.Name] {
						continue
					}
					seenSvc[svc.Name] = true
					// Diff the quadlet so the safety net restarts a running
					// custom service whose content changed this run, e.g. a
					// family/tuning service gaining the new tuning Volume= mount
					// on a v1.22.1 upgrade. Without this the mount lands on disk
					// but the live container keeps its old config until a manual
					// restart, like rewriteDefaultPreset already handles.
					path := filepath.Join(config.QuadletDir(), "servlo-"+svc.Name+".container")
					before, _ := os.ReadFile(path)
					ensureServiceQuadlet(svc.Name) //nolint:errcheck
					if after, _ := os.ReadFile(path); string(before) != string(after) {
						changedQuadlets = append(changedQuadlets, "servlo-"+svc.Name)
					}
				}
			}
		}

		refreshUnreferencedCustomQuadlets(seenSvc, reg)

		// Make sure every installed PHP version (not just the default and
		// registered-site versions) picks up the current FPM template — the
		// debug bridge moved to an always-mounted layout in v1.20 and existing
		// quadlets need one rewrite to gain the new Volume= lines. Cheap
		// no-op for versions that are already up to date.
		if err := podman.RewriteFPMQuadlets(); err != nil {
			fmt.Printf("  WARN: refreshing FPM quadlets: %v\n", err)
		}
	}

	// 7. Pull images before the containers that need them.
	pullJobs := []BuildJob{
		{
			Label: "Pulling nginx:alpine",
			Run: func(w io.Writer) error {
				cmd := podman.Cmd("pull", "docker.io/library/nginx:alpine")
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
	}
	for _, job := range pullJobs {
		step(job.Label)
		if err := job.Run(io.Discard); err != nil {
			fmt.Printf("WARN: %v\n", err)
			continue
		}
		ok()
	}

	// Pull and build every service and FPM image before starting anything.
	if servloSystemd.IsAutostartEnabled() {
		ensureImages()
	}

	// 8. Systemd / services
	step("Reloading service manager")
	if err := services.Mgr.DaemonReload(); err != nil {
		return err
	}
	ok()

	// Start containers removed by the network recreate. Runs after the
	// quadlet refresh + DaemonReload so they come up on fresh quadlets.
	migratedSet := make(map[string]bool, len(migrated))
	for _, c := range migrated {
		migratedSet[c] = true
		step("starting " + c + " (network migration)")
		if err := podman.StartUnit(c); err != nil {
			feedback.Warn("%v", err)
		} else {
			ok()
		}
	}

	// Migration safety net: restart any container whose quadlet content
	// actually changed during this install run, except servlo-nginx (handled
	// separately) and anything we just started above.
	for _, name := range changedQuadlets {
		if name == "servlo-nginx" || migratedSet[name] {
			continue
		}
		if running, _ := podman.ContainerRunning(name); !running {
			continue
		}
		step("restarting " + name + " (PublishPort changed)")
		if err := services.Mgr.Restart(name); err != nil {
			feedback.Warn("%v", err)
		} else {
			ok()
		}
	}

	// Read the autostart flag once. When disabled (set explicitly via
	// `servlo autostart disable`), install must not enable or start any
	// service that the user has chosen to keep off — otherwise running
	// `servlo update` would silently flip every disabled unit back on.
	// The zero value (Disabled=false) is the historical autostart-on
	// path, so existing users see no behaviour change.
	autostartOn := servloSystemd.IsAutostartEnabled()

	if autostartOn {
		step("Starting servlo-nginx")
		if err := services.Mgr.Restart("servlo-nginx"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
		ok()
	}

	step("Writing watcher service")
	if content, err := servloSystemd.GetUnit("servlo-watcher"); err == nil {
		if err := writeUserServiceWithReload("servlo-watcher", content); err != nil {
			return err
		}
		if autostartOn {
			if err := services.Mgr.Enable("servlo-watcher"); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
		}
	}
	ok()

	if autostartOn {
		step("Restarting watcher service")
		if err := services.Mgr.Restart("servlo-watcher"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
		ok()
	}

	step("Writing UI service")
	if content, err := servloSystemd.GetUnit("servlo-panel"); err == nil {
		if err := writeUserServiceWithReload("servlo-panel", content); err != nil {
			return err
		}
		if autostartOn {
			if err := services.Mgr.Enable("servlo-panel"); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
		}
	}
	ok()

	if autostartOn {
		step("Starting servlo-panel")
		if err := services.Mgr.Restart("servlo-panel"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
		ok()
	}

	// Restore worker / queue / schedule unit FILES from .servlo.yaml so the
	// systemd state is repaired regardless of the autostart setting — the
	// files have to exist for the user to be able to flip autostart back
	// on later. restoreSiteInfrastructure only writes files for units
	// that don't already exist, so this is a no-op for ordinary updates.
	restoreSiteInfrastructure()

	// Ensure every globally configured service has its unit file on disk,
	// which a clean install restored from a config backup does not.
	migrateServiceUnits()

	// Build any missing derived FrankenPHP image regardless of autostart: the
	// quadlet refresh above already rewrote FrankenPHP quadlets to point at the
	// localhost derived image, so the image must exist or the next container
	// restart (reboot, manual, a php.ini save) would reference a missing image.
	// BuildFrankenPHPImage no-ops when the image is already current; it never
	// restarts a container, so it's safe to run with autostart disabled.
	for _, v := range activeFrankenPHPVersions() {
		if err := podman.BuildFrankenPHPImage(v, false, os.Stdout); err != nil {
			fmt.Printf("  WARN: building FrankenPHP image for PHP %s: %v\n", v, err)
		}
	}

	// Start service containers and workers only when autostart is on.
	// When the user has explicitly disabled autostart we leave them
	// stopped — `servlo update` running install via re-exec must not flip
	// disabled units back on.
	if autostartOn {
		// Rebuild FPM images when the embedded Containerfile changed since
		// the last build — BuildFPMImage no-ops when the image already
		// exists, so `brew upgrade && servlo install` would otherwise ship
		// new binary against stale images. Gated by autostartOn because
		// php:rebuild restarts FPM and worker units unconditionally.
		activeFPM, _ := phpDet.ListInstalled()
		if podman.NeedsFPMRebuild(activeFPM) || podman.NeedsFrankenPHPRebuild(activeFrankenPHPVersions()) {
			feedback.Header("Rebuilding PHP images")
			self, err := os.Executable()
			if err != nil {
				fmt.Printf("  WARN: locating servlo binary for php:rebuild: %v\n", err)
			} else {
				rebuildCmd := exec.Command(self, "php:rebuild")
				rebuildCmd.Stdout = os.Stdout
				rebuildCmd.Stderr = os.Stderr
				rebuildCmd.Stdin = os.Stdin
				if err := rebuildCmd.Run(); err != nil {
					fmt.Printf("  WARN: php:rebuild failed: %v\n", err)
				}
			}
		}

		// Start installed PHP FPM containers whose images are now available.
		if len(activeFPM) > 0 {
			var fpmJobs []BuildJob
			for _, v := range activeFPM {
				ver := v
				short := strings.ReplaceAll(ver, ".", "")
				if podman.RunSilent("image", "exists", "servlo-php"+short+"-fpm:local") != nil {
					continue // image still missing, skip
				}
				unit := "servlo-php" + short + "-fpm"
				fpmJobs = append(fpmJobs, BuildJob{
					Label: "php" + short + "-fpm",
					Run:   func(_ io.Writer) error { return podman.StartUnit(unit) },
				})
			}
			if len(fpmJobs) > 0 {
				feedback.Header("Starting PHP-FPM")
				RunParallel(fpmJobs) //nolint:errcheck
			}
		}

		startRestoredServices()
		startPerSiteContainers()
	}

	if wantLaravelInstaller {
		step("installing Laravel installer")
		if err := installLaravelInstaller(); err != nil {
			feedback.Warn("%v", err)
		} else {
			ok()
		}
	}

	installAutostart()
	installCleanupScript()

	step("Adding shell PATH configuration")
	if err := addShellShims(wantServloNode); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	if err := shims.Reconcile(clientShimPrompter()); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	if wantServloNode {
		ensureDefaultNode()
	}

	// Re-sync host workers to the current JS runtime once the Node-management
	// state is finalized. This makes "install bun, then `servlo update`" switch
	// Vite and friends onto bun with no manual re-link, and makes answering the
	// management question move existing workers onto the manager that answer
	// selects, the way node:manage / node:unmanage already do. Gated on autostart
	// (don't start units the user disabled); change-detection inside means only
	// workers whose command actually changes get restarted.
	if autostartOn && (bunPath != "" || nodeStateChanged) {
		regenerateHostWorkers()
	}

	refreshStoreFrameworks()
	refreshStorePresets()

	feedback.Begin()
	feedback.Done("servlo installation complete")
	feedback.Begin()
	feedback.Note("Dashboard: " + feedback.Val("http://servlo.localhost"))
	feedback.Note("Terminal:  " + feedback.Val("servlo tui"))
	feedback.Begin()
	return nil
}

// writeUserServiceWithReload writes a user service unit file and reloads
// systemd when the on-disk content changed, so the next Start/Restart
// picks up the new directives instead of the cached pre-write copy.
func writeUserServiceWithReload(name, content string) error {
	changed, err := services.Mgr.WriteServiceUnitIfChanged(name, content)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		fmt.Printf("    WARN: daemon-reload after %s: %v\n", name, err)
	}
	return nil
}

// startPerSiteContainers starts units for per-site custom containers and
// FrankenPHP runtimes. startRestoredServices only covers global services, so
// without this, uninstall+reinstall leaves these quadlets stopped on disk.
func startPerSiteContainers() {
	units := installedCustomContainerUnits()
	if len(units) == 0 {
		return
	}
	jobs := make([]BuildJob, len(units))
	for i, u := range units {
		unit := u
		label := strings.TrimPrefix(unit, "servlo-")
		jobs[i] = BuildJob{
			Label: label,
			Run:   func(_ io.Writer) error { return podman.StartUnit(unit) },
		}
	}
	feedback.Header("Starting per-site containers")
	RunParallel(jobs) //nolint:errcheck
}

// refreshUnreferencedCustomQuadlets rewrites quadlets for globally installed
// custom services, per-site custom containers, and per-site FrankenPHP
// containers the earlier per-site walk would skip, so schema changes reach every managed container.
// writeCustomFPMQuadletFn is the seam tests override to assert the refresh
// routes a custom-FPM site to its writer without spawning podman.
var writeCustomFPMQuadletFn = podman.WriteCustomFPMQuadlet

func refreshUnreferencedCustomQuadlets(seenSvc map[string]bool, reg *config.SiteRegistry) {
	if customs, err := config.ListCustomServices(); err == nil {
		for _, svc := range customs {
			if seenSvc[svc.Name] {
				continue
			}
			seenSvc[svc.Name] = true
			ensureCustomServiceQuadlet(svc) //nolint:errcheck
		}
	}
	if reg == nil {
		return
	}
	for _, s := range reg.Sites {
		if s.Paused || s.Ignored {
			continue
		}
		switch {
		case s.IsCustomContainer():
			if err := podman.WriteCustomContainerQuadlet(s.Name, s.Path, s.ContainerPort); err != nil {
				fmt.Printf("  WARN: refreshing %s quadlet: %v\n", podman.CustomContainerName(s.Name), err)
			}
		case s.IsFrankenPHP():
			// A site stuck on a sub-8.2 PHP with the FrankenPHP runtime (only
			// reachable on registries written before the version guards landed)
			// would otherwise be rebuilt at a normalized-up image every refresh,
			// silently running a different PHP than it reports. Heal it to FPM.
			if !config.IsFrankenPHPVersion(s.PHPVersion) {
				site := s
				if err := siteops.DemoteFrankenPHPToFPM(&site); err != nil {
					fmt.Printf("  WARN: switching %s off FrankenPHP (no PHP %s image): %v\n", s.Name, s.PHPVersion, err)
				}
				continue
			}
			entrypoint, env := s.FrankenPHPQuadletSpec()
			if err := podman.WriteFrankenPHPQuadlet(s.Name, s.Path, s.PHPVersion, entrypoint, env); err != nil {
				fmt.Printf("  WARN: refreshing %s quadlet: %v\n", podman.FrankenPHPContainerName(s.Name), err)
			}
		case s.IsCustomFPM():
			// Without this branch a custom-FPM site's quadlet is only rewritten on
			// link, so an upgrade never back-fills additions to the FPM template
			// (the shared php.ini mount from #944), and `php:ini shared` reports
			// success while the setting silently never reaches the container.
			if err := writeCustomFPMQuadletFn(s.Name, s.PHPVersion); err != nil {
				fmt.Printf("  WARN: refreshing %s quadlet: %v\n", podman.CustomFPMContainerName(s.Name), err)
			}
		}
	}
}

// ensureSystemdLinger checks whether systemd user linger is enabled for the
// current user and runs `sudo loginctl enable-linger` if not. Without linger
// the rootless Podman containers (servlo-nginx, PHP-FPM, …) get torn
// down by systemd-logind when the session goes inactive — screen blank,
// lock, switch user, logout — and servlo appears to silently stop working
// until the user manually re-runs `servlo install` or restarts the units.
//
// We only act on a clear "Linger=no" reading. If loginctl is missing or its
// output is unparseable (non-systemd init, container without logind, …) we
// silently skip rather than fail the install.
func ensureSystemdLinger() error {
	if !defaultLingerNeeded() {
		return nil
	}
	user := currentUserName()

	feedback.Warn("systemd user linger is disabled for this account")
	feedback.Note("without it, servlo's containers (DNS, nginx, PHP-FPM) are torn down by")
	feedback.Note("systemd-logind on screen blank, lock, or logout, and servlo will appear")
	feedback.Note("to stop working until you manually restart it")
	step("enabling linger via `sudo loginctl enable-linger " + user + "`")
	fmt.Println()

	cmd := exec.Command("sudo", "loginctl", "enable-linger", user)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println()
		return fmt.Errorf("enabling linger: %w", err)
	}
	ok()
	return nil
}

// checkUnattendedSupported refuses --unattended where the other half of the
// arrangement does not exist. The flag skips the sudo-gated steps because
// `servlo bootstrap --system` and `--trust-ca` do them as root around it, and
// bootstrap is Linux-only; anywhere else the flag would silently leave the
// resolver grant unwritten and the CA untrusted, which reads as broken HTTPS
// and a watcher asking for a password rather than as a missing feature.
func checkUnattendedSupported(unattended bool) error {
	if !unattended || runtime.GOOS == "linux" {
		return nil
	}
	return fmt.Errorf("--unattended is for package installs on Linux, where `servlo bootstrap` applies the root-level setup around it; run `servlo install` without it")
}

// currentUserName resolves the login name the per-user setup steps apply to.
func currentUserName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("LOGNAME")
}

// defaultLingerNeeded acts only on a clear "Linger=no". A missing or
// unparseable loginctl reads as nothing to do rather than as a failure.
func defaultLingerNeeded() bool {
	user := currentUserName()
	if user == "" {
		return false
	}
	if _, err := exec.LookPath("loginctl"); err != nil {
		return false
	}
	out, err := exec.Command("loginctl", "show-user", user).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Linger=no")
}

// reportServerBasics prints the swap, timezone, unattended-upgrades and
// fail2ban steps this machine still needs, with the exact commands. It never
// runs them: they need root, and an install that silently reconfigured a
// machine's timezone and package policy would be doing more than it was asked.
func reportServerBasics() {
	outstanding := serverbasics.Outstanding()
	if len(outstanding) == 0 {
		return
	}
	feedback.Note("server basics this machine is missing:")
	for _, plan := range outstanding {
		feedback.Line("  " + plan.Name + " — " + plan.Detail)
		for _, c := range plan.ForHuman() {
			feedback.Line("    " + c)
		}
	}
	feedback.Note("or apply them all at once with: sudo servlo bootstrap --system")
}

// applyPortStrategy decides how nginx will reach 80 and 443 on this host,
// records the choice with the ports it implies, and prints what the operator
// has to run. It never runs any of it: acquiring privilege behind the
// operator's back is what the no-root-process design exists to avoid, so the
// commands are output, not actions.
//
// It does not fail the install when the host is not ready. The rest of the
// install is per-user and correct either way, and refusing here would leave the
// operator with a half-configured machine and a command to run anyway. servlo
// doctor re-checks the recorded strategy on every run.
func applyPortStrategy() error {
	plan := ports.Detect()

	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		cfg.SetPortStrategy(string(plan.Strategy), plan.HTTPPort, plan.HTTPSPort)
		if err := config.SaveGlobal(cfg); err != nil {
			return fmt.Errorf("recording the port strategy: %w", err)
		}
	}

	if plan.Satisfied {
		feedback.Note("ports: " + plan.Reason)
		return nil
	}

	feedback.Warn("nginx cannot bind 80 and 443 yet")
	feedback.Note(plan.Reason)
	fmt.Println()
	feedback.Note("run these once, as a user who can sudo:")
	fmt.Println()
	for _, c := range plan.Commands {
		fmt.Println("  " + c)
	}
	fmt.Println()
	feedback.Note("'servlo doctor' re-checks this, so you can run them after the install finishes")
	return nil
}

// downloadBinaries is implemented in install_linux.go.

// laravelInstallerPresent returns true if laravel/installer is already
// installed in the user's composer global vendor directory. The composer
// home is bind-mounted into the FPM container, so the package files live
// on the host and can be detected with a plain stat.
func laravelInstallerPresent() bool {
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(os.Getenv("HOME"), ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}
	_, err := os.Stat(filepath.Join(composerHome, "vendor", "laravel", "installer"))
	return err == nil
}

// installLaravelInstaller runs composer global require laravel/installer
// directly inside an installed PHP-FPM container so the `laravel` CLI is
// available for scaffolding new apps. It bypasses the composer shim because
// the shim relies on cwd-based PHP detection, which does not work when
// install is invoked from a directory with no project metadata.
func installLaravelInstaller() error {
	installed, err := phpDet.ListInstalled()
	if err != nil || len(installed) == 0 {
		return fmt.Errorf("no PHP version installed — install one with `servlo use <version>` first")
	}

	// Prefer the configured default PHP, otherwise use the highest installed.
	version := installed[len(installed)-1]
	if cfg, _ := config.LoadGlobal(); cfg != nil && cfg.PHP.DefaultVersion != "" {
		for _, v := range installed {
			if v == cfg.PHP.DefaultVersion {
				version = v
				break
			}
		}
	}

	short := strings.ReplaceAll(version, ".", "")
	container := "servlo-php" + short + "-fpm"

	if running, _ := podman.ContainerRunning(container); !running {
		if err := podman.StartUnit(container); err != nil {
			return fmt.Errorf("starting %s: %w", container, err)
		}
		// Wait for the container to be ready for exec (systemd starts the
		// podman run -d asynchronously, so the container may not exist yet).
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if r, _ := podman.ContainerRunning(container); r {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if r, _ := podman.ContainerRunning(container); !r {
			return fmt.Errorf("%s did not become ready within 30s", container)
		}
	}

	home := os.Getenv("HOME")
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}

	composerPhar := filepath.Join(config.BinDir(), "composer.phar")
	// --no-interaction prevents composer from blocking on plugin trust prompts
	// (e.g. "Do you trust 'symfony/flex' to execute code?") which would hang
	// the installer with no visible output.
	cmd := podman.Cmd("exec", "-i",
		"--env", "HOME="+home,
		"--env", "COMPOSER_HOME="+composerHome,
		container, "php", composerPhar, "global", "require", "--no-interaction", "laravel/installer",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// servloManagesNode reports whether servlo is managing Node for this host
// (persisted node.managed preference, or the historical node PATH shim).
func servloManagesNode() bool {
	return nodeDet.Managed()
}

// nodeManageDecision resolves whether servlo should manage Node.js for this
// install run. The choice is asked once and then remembered: an explicit saved
// preference always wins silently, and a config predating the field adopts the
// current on-disk shim state as that choice without asking. Only a genuine
// first-time install (no saved preference and no shim) prompts, and then only
// when the user already has a Node setup of their own; with none it defaults to
// managed. An nvm install counts as one even when no version is installed into
// it yet, which the system-node probe cannot see. Update never prompts. When
// needPrompt is true the caller runs the prompt and overrides want with the
// answer.
//
// A package install cannot ask, so it answers by detection instead: an existing
// nvm drives Node, otherwise servlo manages it through fnm. It must not fall
// through to the update branch, which reads the answer off the shim state. On a
// first install there is no shim yet, so that recorded "unmanaged" and left the
// package to provision no Node at all, deferring it to whenever something first
// needed one.
func nodeManageDecision(fromUpdate, unattended bool, saved *bool, systemNodeDetected, nvmDetected, shimPresent bool) (want, needPrompt, promptDefault bool) {
	if saved != nil {
		return *saved, false, *saved
	}
	if shimPresent {
		return true, false, true
	}
	if unattended {
		return !nvmDetected, false, !nvmDetected
	}
	if fromUpdate {
		return false, false, false
	}
	if systemNodeDetected || nvmDetected {
		return true, true, true
	}
	return true, false, true
}

// nodeManagerChoice picks the Node version manager to drive. A saved choice is
// never revisited. Otherwise it follows the one management question: servlo-managed
// Node uses the bundled fnm, since managing means owning the versions in servlo's
// own tool, and declining hands Node back to an existing nvm so `servlo npm`, npx
// and setup runs follow it instead of an fnm no version is ever installed into.
// Users who want servlo to manage Node through their nvm switch with
// `servlo node:manager nvm` or the dashboard.
func nodeManagerChoice(saved string, wantServloNode, nvmDetected bool) string {
	if saved != "" {
		return saved
	}
	if !wantServloNode && nvmDetected {
		return "nvm"
	}
	return "fnm"
}

// nodeStateFlipped reports whether this install run changed which Node host
// workers should run: the management answer moved, or the version manager did.
// An empty prevManager is a config predating the setting, which meant fnm, so a
// first-time write of "fnm" is not a flip.
func nodeStateFlipped(prevManaged bool, prevManager string, managed bool, manager string) bool {
	if prevManager == "" {
		prevManager = "fnm"
	}
	return prevManaged != managed || prevManager != manager
}

// ensureNodeManaged is called by the node:install/use/uninstall commands to
// guard against running version-manager operations while the user has opted out of
// servlo-managed Node. Prompts for confirmation and writes shims on accept.
// Returns an error when stdin is not a TTY so scripted callers fail loudly
// instead of silently flipping the user's choice.
func ensureNodeManaged() error {
	if servloManagesNode() {
		return nil
	}
	if fi, err := os.Stdin.Stat(); err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return fmt.Errorf("servlo is not managing Node.js; run 'servlo install' to enable it")
	}
	fmt.Println("Servlo is currently using your system Node.js.")
	if nodeDet.WritesPathShims(nodeDet.Active()) {
		fmt.Println("Continuing will install servlo-managed shims into", config.BinDir(), "and override your system node, npm and npx in PATH.")
	} else {
		fmt.Println("Continuing will let servlo drive your existing nvm for install/use/default (your shell's nvm keeps owning node/npm/npx on PATH).")
	}
	if !confirmInstallPromptDefault("Switch to servlo-managed Node.js?", false) {
		return fmt.Errorf("aborted")
	}
	if err := addShellShims(true); err != nil {
		return fmt.Errorf("writing shims: %w", err)
	}
	persistNodeManaged(true)
	return nil
}

// ensureDefaultNode installs the configured default Node.js version via the
// active version manager and pins it as the default if no version is already set
// up. Skips when the manager already has a working default so reruns of
// `servlo install` stay quiet.
func ensureDefaultNode() {
	mgr := nodeDet.Active()
	if !mgr.Available() {
		fmt.Printf("    WARN: %s not found, skipping default Node install\n", mgr.Name())
		return
	}
	if mgr.HasDefault() {
		return
	}
	version := "22"
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil && cfg.Node.DefaultVersion != "" {
		version = cfg.Node.DefaultVersion
	}
	step(fmt.Sprintf("Installing Node.js %s", version))
	if err := mgr.Install(version); err != nil {
		fmt.Printf("    WARN: %v\n", err)
		return
	}
	if err := mgr.SetDefault(version); err != nil {
		fmt.Printf("    WARN: %v\n", err)
		return
	}
	ok()
}

// detectSystemNode returns a hint about an existing node install outside of
// servlo's own bin dir, or "" if none can be found. Probes node/npm/npx in PATH
// and well-known version-manager directories (nvm, volta, mise, asdf, fnm),
// since most version managers inject node via a shell hook rather than a
// static PATH entry and would otherwise be invisible here.
func detectSystemNode() string {
	servloBin := config.BinDir()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == servloBin {
			continue
		}
		for _, bin := range []string{"node", "npm", "npx"} {
			candidate := filepath.Join(dir, bin)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		for _, rel := range []string{
			".nvm/versions/node",
			".volta/bin",
			".local/share/mise/installs/node",
			".asdf/installs/nodejs",
			".local/share/fnm/node-versions",
			"Library/Application Support/fnm/node-versions",
		} {
			p := filepath.Join(home, rel)
			if entries, err := os.ReadDir(p); err == nil && len(entries) > 0 {
				return p
			}
		}
	}
	return ""
}

// detectNvm reports whether a user-installed nvm is present, so declining
// servlo-managed Node can hand Node back to it instead of the bundled fnm. Both
// layout that counts is the script install's $NVM_DIR/nvm.sh, which
// keeps nvm.sh under its own prefix and leaves NVM_DIR holding only the
// versions.
func detectNvm() bool {
	return nodeDet.ScriptPresent()
}

// confirmInstallPrompt asks a [Y/n] question. Must be called before any
// RunParallel invocation, which leaves a goroutine reading from os.Stdin.
func confirmInstallPrompt(question string) bool {
	return confirmInstallPromptDefault(question, true)
}

// confirmInstallPromptDefault is like confirmInstallPrompt but lets the caller
// pick the default for an empty answer, so re-running install can mirror the
// user's previous choice. Falls back to /dev/tty when stdin is not a TTY so
// prompts still work when servlo is piped, e.g. `curl ... | bash` -> `servlo install`.
func confirmInstallPromptDefault(question string, defaultYes bool) bool {
	src, closer, ok := promptSource()
	if !ok {
		ans := "yes"
		if !defaultYes {
			ans = "no"
		}
		feedback.Prompt(question, defaultYes)
		fmt.Println(feedback.Dim("(no terminal, defaulting to " + ans + ")"))
		return defaultYes
	}
	if closer != nil {
		defer closer.Close()
	}
	return readConfirmAnswer(src, question, defaultYes)
}

// promptSource returns a reader suitable for interactive prompts. It prefers
// os.Stdin when it is a TTY, otherwise opens /dev/tty. The returned closer is
// non-nil only for /dev/tty and must be closed by the caller.
func promptSource() (io.Reader, io.Closer, bool) {
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return os.Stdin, nil, true
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		return tty, tty, true
	}
	return nil, nil, false
}

func readConfirmAnswer(r io.Reader, question string, defaultYes bool) bool {
	feedback.Prompt(question, defaultYes)
	answer := strings.TrimSpace(strings.ToLower(readLine(r)))
	if answer == "" {
		return defaultYes
	}
	return answer != "n" && answer != "no"
}

// readLine reads a single line, one byte at a time, stopping after the first
// newline. Reading unbuffered is deliberate: successive prompts share the
// terminal, so a buffered reader would pull typed-ahead input past this line and
// discard it, making the next prompt block. Byte-at-a-time leaves the rest in
// the terminal buffer for the following prompt.
func readLine(r io.Reader) string {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}

func addShellShims(manageNode bool) error {
	home, _ := os.UserHomeDir()
	binDir := config.BinDir()
	// Use the running binary so shims work regardless of install method
	// (the installer's ~/.local/bin/servlo, a distribution package's /usr/bin, etc.).
	servloBin, _ := os.Executable()
	if servloBin == "" {
		servloBin = filepath.Join(home, ".local", "bin", "servlo")
	}

	// Write php shim
	phpShim := fmt.Sprintf("#!/bin/sh\nexec %s php \"$@\"\n", servloBin)
	if err := os.WriteFile(filepath.Join(binDir, "php"), []byte(phpShim), 0755); err != nil {
		return fmt.Errorf("writing php shim: %w", err)
	}

	// Write composer shim. Routes through `servlo composer` so global installs
	// land in servlo's bin dir as wrappers (mirroring the npm flow), falling
	// back to a direct `servlo php composer.phar` invocation when the servlo
	// binary is not reachable (containers where the glibc binary can't run).
	if err := os.WriteFile(filepath.Join(binDir, "composer"), []byte(composerShimScript(servloBin, home)), 0755); err != nil {
		return fmt.Errorf("writing composer shim: %w", err)
	}

	// Write laravel shim (laravel/installer global package)
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}
	laravelShim := fmt.Sprintf("#!/bin/sh\nexec %s php %s/vendor/bin/laravel \"$@\"\n", servloBin, composerHome)
	if err := os.WriteFile(filepath.Join(binDir, "laravel"), []byte(laravelShim), 0755); err != nil {
		return fmt.Errorf("writing laravel shim: %w", err)
	}

	// Write node/npm/npx PATH shims only when the active manager needs them.
	// fnm has no shell hook, so the shims are how `node` on PATH reaches fnm.
	// nvm is already loaded by the user's shell; putting servlo wrappers ahead of
	// it makes `nvm ls` / `nvm use` hang, so managed-nvm only removes any stale
	// shims and leaves PATH to nvm. CLI (`servlo node`/`npm`) and host workers
	// still drive nvm through Active() either way.
	// When manageNode is false, existing shims are removed so a prior managed
	// install stops masking the user's node.
	if manageNode && nodeDet.WritesPathShims(nodeDet.Active()) {
		mgr := nodeDet.Active()
		for _, bin := range []string{"node", "npm", "npx"} {
			shim := mgr.ShimScript(servloBin, bin)
			if err := os.WriteFile(filepath.Join(binDir, bin), []byte(shim), 0755); err != nil {
				return fmt.Errorf("writing %s shim: %w", bin, err)
			}
		}
	} else {
		for _, bin := range []string{"node", "npm", "npx"} {
			if err := os.Remove(filepath.Join(binDir, bin)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing %s shim: %w", bin, err)
			}
		}
	}

	if pathShimDisabled() {
		removeShellPathEntry(home)
	} else if err := writeShellPathEntry(home, binDir); err != nil {
		return err
	}
	installShellCompletions(home, servloBin)
	return nil
}

// composerShimScript is the ~/.local/share/servlo/bin/composer script: delegate
// to `servlo composer` when the binary runs here, fall back to driving
// composer.phar directly when it does not (containers where the glibc binary
// cannot execute).
//
// Its own function so a test can run it rather than only read it. The rename
// renamed the branch that reads SERVLO but not the line that assigns it, so the
// variable was empty, the test never passed, and every composer call took the
// fallback route, which does not produce the bin-dir wrappers global installs
// need. The test that was supposed to cover this asserted the delegate line was
// present, which it was.
func composerShimScript(servloBin, home string) string {
	return fmt.Sprintf("#!/bin/sh\nSERVLO=%q\nif [ -x \"$SERVLO\" ]; then\n  exec \"$SERVLO\" composer \"$@\"\nfi\nexec %s php %s/.local/share/servlo/bin/composer.phar \"$@\"\n", servloBin, servloBin, home)
}

// pathShimDisabled reports whether the user opted out of the shell PATH entry
// (`servlo path:disable`). Best-effort: an unreadable config keeps the default.
func pathShimDisabled() bool {
	cfg, err := config.LoadGlobal()
	return err == nil && cfg != nil && cfg.Shims.PathDisabled
}

// writeShellPathEntry puts servlo's bin dir on the PATH of the user's shell:
// an rc export for bash/zsh, a dedicated conf.d file for fish.
func writeShellPathEntry(home, binDir string) error {
	shell := os.Getenv("SHELL")
	switch {
	case isShell(shell, "fish"):
		fishConfigDir := filepath.Join(home, ".config", "fish", "conf.d")
		if err := os.MkdirAll(fishConfigDir, 0755); err != nil {
			return err
		}
		content := fmt.Sprintf("set -gx PATH %s $PATH\n", binDir)
		return os.WriteFile(filepath.Join(fishConfigDir, "servlo.fish"), []byte(content), 0644)
	case isShell(shell, "zsh"):
		return appendShellRC(filepath.Join(home, ".zshrc"), binDir)
	default:
		return appendShellRC(bashRCPath(home), binDir)
	}
}

// removeShellPathEntry removes the PATH entry writeShellPathEntry wrote, in
// every shell's location so a shell switch leaves nothing behind: the "# Servlo"
// block in bash/zsh rc files and the PATH line in fish's conf.d/servlo.fish
// (deleting the file when nothing else remains). The installer's "# Added by
// Servlo installer" block is left alone — it puts the servlo binary itself on
// PATH, not the shims.
func removeShellPathEntry(home string) {
	for _, rc := range []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".zshrc"),
	} {
		removeMarkedBlock(rc, "# Servlo", 1)
	}
	fishConf := filepath.Join(home, ".config", "fish", "conf.d", "servlo.fish")
	data, err := os.ReadFile(fishConf)
	if err != nil {
		return
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "set -gx PATH ") &&
			strings.Contains(line, config.BinDir()) {
			continue
		}
		kept = append(kept, line)
	}
	rest := strings.Join(kept, "\n")
	if strings.TrimSpace(rest) == "" {
		os.Remove(fishConf) //nolint:errcheck
		return
	}
	if rest != string(data) {
		os.WriteFile(fishConf, []byte(rest), 0644) //nolint:errcheck
	}
}

// installShellCompletions installs the completion script for the user's shell.
// Kept separate from the PATH entry so completions for `servlo` itself survive
// path:disable.
func installShellCompletions(home, servloBin string) {
	shell := os.Getenv("SHELL")
	switch {
	case isShell(shell, "fish"):
		installCompletion(servloBin, "fish", filepath.Join(home, ".config", "fish", "completions"), "servlo.fish")
	case isShell(shell, "zsh"):
		zshFunctionsDir := filepath.Join(home, ".local", "share", "zsh", "site-functions")
		if err := os.MkdirAll(zshFunctionsDir, 0755); err == nil {
			installCompletion(servloBin, "zsh", zshFunctionsDir, "_servlo")
			ensureZshFpath(filepath.Join(home, ".zshrc"), zshFunctionsDir)
		}
	default:
		bashCompDir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
		if err := os.MkdirAll(bashCompDir, 0755); err == nil {
			installCompletion(servloBin, "bash", bashCompDir, "servlo")
		}
	}
}

// bashRCPath picks the bash startup file servlo writes its PATH line to.
// Interactive bash reads .bashrc.
func bashRCPath(home string) string {
	return filepath.Join(home, ".bashrc")
}

func appendShellRC(rcFile, binDir string) error {
	data, _ := os.ReadFile(rcFile)
	line := fmt.Sprintf("export PATH=\"%s:$PATH\"", binDir)
	if strings.Contains(string(data), line) {
		return nil
	}
	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(fmt.Sprintf("\n# Servlo\n%s\n", line))
	return err
}

func isShell(shell, name string) bool {
	return len(shell) > 0 && filepath.Base(shell) == name
}

// installCompletion generates and writes a shell completion script for servlo.
func installCompletion(servloBin, shell, dir, filename string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	// Skip if servloBin looks like a test binary to avoid re-entering test code.
	if strings.HasSuffix(servloBin, ".test") || strings.Contains(servloBin, "/tmp/") {
		return
	}
	out, err := exec.Command(servloBin, "completion", shell).Output()
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, filename), out, 0644) //nolint:errcheck
}

// ensureZshFpath appends a fpath line for dir to the zshrc if not already present.
func ensureZshFpath(zshrc, dir string) {
	data, _ := os.ReadFile(zshrc)
	line := fmt.Sprintf("fpath=(%s $fpath)", dir)
	if strings.Contains(string(data), line) {
		return
	}
	f, err := os.OpenFile(zshrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n# Servlo completions\n%s\nautoload -Uz compinit && compinit\n", line)
}
