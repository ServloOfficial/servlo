package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/cleanup"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dnscheck"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/nginx"
	phpPkg "github.com/ServloOfficial/servlo/internal/php"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/ports"
	"github.com/ServloOfficial/servlo/internal/serverbasics"
	"github.com/ServloOfficial/servlo/internal/services"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
	servloUpdate "github.com/ServloOfficial/servlo/internal/update"
	"github.com/ServloOfficial/servlo/internal/version"
	"github.com/ServloOfficial/servlo/pkg/distro"
	"github.com/spf13/cobra"
)

// NewDoctorCmd returns the doctor command.
func NewDoctorCmd() *cobra.Command {
	var fix, yes, dryRun, asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose your Servlo environment and report issues",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDoctor(fix, yes, dryRun, asJSON)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "Offer to apply the automatic repairs for any findings")
	cmd.Flags().BoolVar(&yes, "yes", false, "With --fix, apply fixes without prompting (heavy fixes still confirm)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "With --fix, show what would be repaired without changing anything")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the findings as JSON (each carries a fix tier), instead of the human report")
	return cmd
}

func runDoctor(fix, yes, dryRun, asJSON bool) error {
	if asJSON {
		rep, err := RunDoctorReport()
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	useColor := feedback.Animated()
	rep, err := runDoctorInto(os.Stdout, useColor)
	if err != nil {
		return err
	}
	if !fix {
		return nil
	}
	return runDoctorFix(os.Stdout, rep, yes, dryRun)
}

// RunDoctorTo runs the full doctor diagnostic, writing human-readable output
// to w. When useColor is false ANSI escapes are stripped so the output is
// safe to embed in a plain-text file (used by `servlo bug-report`). Returns
// the failure and warning counts for callers that want to summarise.
func RunDoctorTo(w io.Writer, useColor bool) (fails, warns int, err error) {
	rep, err := runDoctorInto(w, useColor)
	return rep.Failures, rep.Warnings, err
}

// RunDoctorReport runs the full diagnostic without printing and returns the
// structured findings, used by `servlo doctor --fix` and the diag tool.
func RunDoctorReport() (DoctorReport, error) {
	return runDoctorInto(io.Discard, false)
}

func runDoctorInto(w io.Writer, useColor bool) (DoctorReport, error) {
	rep := &DoctorReport{Version: version.String()}
	section := ""
	ok := func(label string) {
		fmt.Fprintf(w, "  %s %s\n", feedback.GreenIf(useColor, feedback.GlyphOK), label)
		rep.add(Finding{Section: section, Name: label, Status: "ok"})
	}
	fail := func(label, msg, hint string) {
		rep.Failures++
		fmt.Fprintf(w, "  %s %s  %s\n    hint: %s\n", feedback.RedIf(useColor, feedback.GlyphFail), label, msg, hint)
		rep.add(Finding{Section: section, Name: label, Status: "fail", Message: msg, Hint: hint})
	}
	warn := func(label, msg string) {
		rep.Warnings++
		fmt.Fprintf(w, "  %s %s  %s\n", feedback.AmberIf(useColor, feedback.GlyphWarn), label, msg)
		rep.add(Finding{Section: section, Name: label, Status: "warn", Message: msg})
	}
	info := func(label, val string) {
		fmt.Fprintf(w, "  %-34s %s\n", label, val)
		rep.add(Finding{Section: section, Name: strings.TrimSpace(label), Status: "info", Message: val})
	}

	fmt.Fprintf(w, "Servlo Doctor  (version %s)\n", version.String())
	fmt.Fprintln(w, "══════════════════════════════════════════════")

	// ── Prerequisites ───────────────────────────────────────────────────────
	section = "Prerequisites"
	fmt.Fprintln(w, "\n[Prerequisites]")

	// The platform gate is first because everything below it assumes Ubuntu:
	// the package hints, the trust store path and the systemd layout are all
	// written for it, so a wrong platform makes every later finding misleading.
	if d, dErr := distro.Detect(); dErr != nil {
		warn("platform", "could not read /etc/os-release: "+dErr.Error())
	} else if supErr := d.Supported(); supErr != nil {
		fail("platform", supErr.Error(), "Servlo runs on Ubuntu 24.04 LTS")
		rep.fixLast(manualFix)
	} else {
		ok("platform (" + d.PrettyName + ")")
	}

	if _, lookErr := exec.LookPath("podman"); lookErr != nil {
		fail("podman binary", "not found in PATH", "install podman: https://podman.io/docs/installation")
		rep.fixLast(manualFix)
	} else if runErr := podman.RunSilent("info"); runErr != nil {
		fail("podman", "podman info failed — daemon not running?", podmanDaemonHint())
		rep.fixLast(manualFix)
	} else {
		ok("podman")
	}

	// podman 4.5 is servlo's minimum on every platform: older clients reject the
	// quadlet units servlo emits and hit build regressions (#636). Probes the
	// binary directly, so it reports even when the daemon/machine is down.
	if meetsMin, ver, verErr := podman.VersionAtLeast(4, 5); verErr == nil {
		if meetsMin {
			ok(fmt.Sprintf("podman version (%s)", ver))
		} else {
			fail("podman version", "podman "+ver+" is older than the 4.5 minimum",
				"upgrade podman to 4.5 or newer: https://podman.io/docs/installation")
			rep.fixLast(manualFix)
		}
	}

	if runtime.GOOS == "linux" {
		if _, lookErr := exec.LookPath("crun"); lookErr != nil {
			warn("OCI runtime", "crun not found — recommended for rootless podman (install: sudo apt install crun)")
			rep.fixLast(manualFix)
		} else {
			ok("OCI runtime (crun)")
		}

		if out, runErr := exec.Command("systemctl", "--user", "is-system-running").Output(); runErr != nil {
			state := strings.TrimSpace(string(out))
			if state == "degraded" {
				warn("systemd user session", "degraded — some units have failed")
			} else {
				fail("systemd user session", fmt.Sprintf("state=%q", state), "log in as a real user (not su); run: systemctl --user status")
			}
		} else {
			ok("systemd user session")
		}

		// Podman orders every rootless quadlet after its network-online wait
		// unit. Where network-online.target is never pulled in (Fedora
		// Silverblue and other atomic images) that unit can only time out, and
		// every container start, plus the boot, pays the 90s.
		if servloSystemd.NetworkWaitStalls() {
			warn("podman network-online wait", "network-online.target never activates here, so every container start stalls 90s — fix: servlo start")
			rep.fixLast(autoFix(fixNetworkWait, "", "install the podman network-online drop-in"))
		} else {
			ok("podman network-online wait")
		}

		currentUser := os.Getenv("USER")
		if currentUser == "" {
			currentUser = os.Getenv("LOGNAME")
		}
		if currentUser != "" {
			out, runErr := exec.Command("loginctl", "show-user", currentUser).Output()
			if runErr != nil || !strings.Contains(string(out), "Linger=yes") {
				warn("linger enabled", "services won't survive logout — fix: loginctl enable-linger "+currentUser)
				rep.fixLast(autoFix(fixEnableLinger, currentUser, "enable lingering so services survive logout"))
			} else {
				ok("linger enabled")
			}
		}

		// Rootless podman build preflight: a missing subuid/subgid range or a
		// missing fuse-overlayfs surface as the same opaque tar "Operation not
		// permitted" failure during image builds (#636). Diagnose each so the
		// operator gets a real pointer rather than that message.
		if currentUser != "" {
			uid := strconv.Itoa(os.Getuid())
			for _, path := range []string{"/etc/subuid", "/etc/subgid"} {
				b, readErr := os.ReadFile(path)
				if readErr != nil || !hasSubIDRange(string(b), currentUser, uid) {
					fail(path+" range", "no sub-id range for "+currentUser+" (rootless podman builds will fail)",
						"add one: echo "+currentUser+":100000:65536 | sudo tee -a "+path+" && podman system migrate")
					rep.fixLast(manualFix)
				} else {
					ok(path + " range")
				}
			}
		}

		if _, lookErr := exec.LookPath("fuse-overlayfs"); lookErr != nil {
			warn("fuse-overlayfs", "not found — recommended for rootless overlay storage (install: sudo apt install fuse-overlayfs)")
			rep.fixLast(manualFix)
		} else {
			ok("fuse-overlayfs")
		}

		// Rootless network helpers. servlo's containers run on a custom bridge
		// network, which on rootless podman requires netavark + aardvark-dns
		// plus a rootless network tool (pasta or slirp4netns). Missing any of
		// these is the "failed to mount runtime directory for rootless netns"
		// container start failure from #635 — a fresh, minimal host can lack
		// them entirely. They need sudo to install, so flag with the command.
		//
		// netavark/aardvark-dns live in libexec, not on $PATH, so ask podman
		// for the paths it actually resolved rather than LookPath (which would
		// false-fail on a healthy host). Skip the check when podman can't report
		// them (older podman without the field).
		if netavark, aardvark, probed := podman.NetworkHelpers(); probed {
			if netavark == "" {
				fail("rootless network (netavark)", "podman cannot find netavark — containers on the servlo bridge cannot start",
					"sudo apt install netavark; then: servlo install")
				rep.fixLast(manualFix)
			} else {
				ok("rootless network (netavark)")
			}
			if aardvark == "" {
				fail("rootless network (aardvark-dns)", "podman cannot find aardvark-dns — container DNS will not resolve",
					"sudo apt install aardvark-dns; then: servlo install")
				rep.fixLast(manualFix)
			} else {
				ok("rootless network (aardvark-dns)")
			}
		}
		// pasta/slirp4netns are user-PATH tools, so LookPath is reliable here.
		if _, p := exec.LookPath("pasta"); p != nil {
			if _, s := exec.LookPath("slirp4netns"); s != nil {
				fail("rootless network (pasta/slirp4netns)", "neither pasta nor slirp4netns found — rootless containers have no network",
					"sudo apt install passt  (provides pasta), or: sudo apt install slirp4netns; then: servlo install")
				rep.fixLast(manualFix)
			} else {
				ok("rootless network (slirp4netns)")
			}
		} else {
			ok("rootless network (pasta)")
		}
	}

	quadletDir := config.QuadletDir()
	if dirErr := checkDirWritable(quadletDir); dirErr != nil {
		fail("service config dir writable", dirErr.Error(), "mkdir -p "+quadletDir)
		rep.fixLast(autoFix(fixMkdir, quadletDir, "create the service config directory"))
	} else {
		ok("service config dir writable")
	}

	dataDir := config.DataDir()
	if dirErr := checkDirWritable(dataDir); dirErr != nil {
		fail("data dir writable", dirErr.Error(), "mkdir -p "+dataDir)
		rep.fixLast(autoFix(fixMkdir, dataDir, "create the data directory"))
	} else {
		ok("data dir writable")
	}

	// ── Configuration ────────────────────────────────────────────────────────
	section = "Configuration"
	fmt.Fprintln(w, "\n[Configuration]")

	cfgFile := config.GlobalConfigFile()
	if _, statErr := os.Stat(cfgFile); os.IsNotExist(statErr) {
		warn("config file", "not found — defaults will be used ("+cfgFile+")")
	} else {
		ok("config file exists")
	}

	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		fail("config loads", cfgErr.Error(), "check "+cfgFile+" for YAML syntax errors")
		cfg = nil
	} else {
		ok("config valid")
	}

	if cfg != nil {
		if cfg.PHP.DefaultVersion == "" {
			warn("PHP default version", "not set in config")
		} else {
			ok(fmt.Sprintf("PHP default version (%s)", cfg.PHP.DefaultVersion))
		}

		if cfg.Nginx.HTTPPort <= 0 || cfg.Nginx.HTTPSPort <= 0 {
			fail("nginx ports", fmt.Sprintf("http=%d https=%d", cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort), "set valid ports in "+cfgFile)
		} else {
			ok(fmt.Sprintf("nginx ports (%d / %d)", cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort))
		}

		for _, dir := range cfg.ParkedDirectories {
			if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
				warn(fmt.Sprintf("parked dir: %s", truncate(dir, 26)), "directory does not exist — run: mkdir -p "+dir)
				rep.fixLast(autoFix(fixMkdir, dir, "create the parked directory"))
			} else {
				ok(fmt.Sprintf("parked dir: %s", truncate(dir, 26)))
			}
		}
	}

	// ── Ports ────────────────────────────────────────────────────────────────
	section = "Ports"
	fmt.Fprintln(w, "\n[Ports]")

	// The port strategy is the one piece of setup servlo asks a human to apply
	// and then never touches again, so it is the most likely to have drifted
	// since. Checked on every run rather than only at install, and in two halves:
	// a strategy that is live but not persisted serves fine today and stops at
	// the next reboot, which is precisely the failure nobody notices until the
	// droplet comes back up.
	strategy := ports.ParseStrategy("")
	httpPort, httpsPort := ports.Publish(strategy)
	if cfg != nil {
		strategy = ports.ParseStrategy(cfg.PortStrategy())
		httpPort, httpsPort = ports.Publish(strategy)

		health := ports.CheckHealth(strategy)
		if health.Healthy() {
			ok(fmt.Sprintf("port strategy (%s)", strategy))
		} else {
			hint := portFixHint(health.Fix)
			fail(fmt.Sprintf("port strategy (%s)", strategy), health.Detail, hint)
			rep.fixLast(manualFixWith(hint))
		}

		// A recorded strategy whose ports disagree with the config means one of
		// the two was changed by hand. Under the nftables strategy nginx on 80
		// would swallow the traffic the redirect is aimed at.
		if cfg.Nginx.HTTPPort != httpPort || cfg.Nginx.HTTPSPort != httpsPort {
			fail("port strategy ports",
				fmt.Sprintf("%s implies %d/%d but config says %d/%d", strategy, httpPort, httpsPort, cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort),
				"re-run 'servlo install' to record the strategy and its ports together")
			rep.fixLast(manualFix)
			httpPort, httpsPort = cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort
		}
	}

	// The ports checked below are the ones nginx actually publishes, which under
	// the nftables strategy are the high ones rather than 80 and 443.
	http, https := strconv.Itoa(httpPort), strconv.Itoa(httpsPort)
	nginxRunning, _ := podman.ContainerRunning("servlo-nginx")
	if nginxRunning {
		ok(fmt.Sprintf("port %s (nginx running)", http))
		ok(fmt.Sprintf("port %s (nginx running)", https))
	} else {
		if PortInUse(http) {
			fail("port "+http, "in use by another process", "find the process: "+FindListenerCmd(http))
		} else {
			ok(fmt.Sprintf("port %s (free)", http))
		}
		if PortInUse(https) {
			fail("port "+https, "in use by another process", "find the process: "+FindListenerCmd(https))
		} else {
			ok(fmt.Sprintf("port %s (free)", https))
		}
	}

	// ── Server basics ────────────────────────────────────────────────────────
	// Re-checked on every run rather than only at install. These are applied
	// once and never touched again, which makes them the most likely to have
	// been undone: a rebuilt droplet, an image that ships its own timezone, a
	// fail2ban that failed to start after an upgrade.
	section = "Server basics"
	fmt.Fprintln(w, "\n[Server basics]")
	for _, plan := range serverbasics.All() {
		if plan.Satisfied {
			ok(plan.Name)
			continue
		}
		hint := strings.Join(plan.ForHuman(), " && ")
		if hint == "" {
			hint = "no automatic fix for this one"
		}
		warn(plan.Name, plan.Detail)
		rep.fixLast(manualFixWith(hint))
	}

	// ── Certificates ─────────────────────────────────────────────────────────
	section = "Certificates"
	fmt.Fprintln(w, "\n[Certificates]")
	{
		ok("issuer (" + certs.IssuerName() + ")")

		// The webroot is a bind mount. Podman creates a missing source as a
		// root-owned directory, which servlo then cannot write tokens into, so
		// an HTTP-01 challenge fails with a permission error that says nothing
		// about certificates. Check it is there and writable before that.
		challengeDir := config.ACMEChallengeDir()
		switch err := nginx.EnsureChallengeDir(); {
		case err != nil:
			fail("ACME challenge webroot", err.Error(),
				"create it and make it yours: mkdir -p "+challengeDir)
			rep.fixLast(manualFix)
		default:
			ok("ACME challenge webroot")
		}

		// A host with no public address on any interface cannot be measured
		// against a DNS record, and every issuance would refuse. That is right
		// for a machine that genuinely is not reachable, and wrong for one
		// behind a load balancer or a floating IP, so say which fix applies.
		if _, addrErr := dnscheck.ServerAddresses(context.Background()); addrErr != nil && len(cfg.ACMEServerAddresses()) == 0 {
			warn("public address", addrErr.Error()+
				" — if this server is reachable through a load balancer or a floating IP, set certs.server_addresses in "+config.ConfigDir())
			rep.fixLast(manualFix)
		} else {
			ok("public address")
		}

		// A renewal that has been failing while the certificate still has weeks
		// on it is the state this exists to surface: everything works, nobody is
		// told, and a month later the site goes down for a reason that stopped
		// being visible back here.
		for _, f := range certs.RenewalFailures() {
			fail("certificate renewal for "+f.Domain,
				fmt.Sprintf("failing since %s: %s", f.Since.Format(time.DateOnly), f.Reason),
				"fix the cause, then: servlo secure --renew "+f.Domain)
			rep.fixLast(manualFix)
		}

		// And what is actually on disk, which a machine restored from a backup
		// can get wrong with no failure record at all.
		secured := securedCertDomains()
		for _, p := range certs.ExpiryProblems(secured) {
			if p.Expired {
				fail("certificate for "+p.Domain, p.Reason, "servlo secure --renew "+p.Domain)
			} else {
				warn("certificate for "+p.Domain, p.Reason)
			}
			rep.fixLast(manualFix)
		}
		if len(secured) > 0 && len(certs.ExpiryProblems(secured)) == 0 && len(certs.RenewalFailures()) == 0 {
			ok(fmt.Sprintf("certificates (%d secured domain(s))", len(secured)))
		}

		if cfg != nil {
			if email, _, _ := cfg.ACMESettings(); email == "" {
				warn("certificate contact email",
					"unset, so the authority cannot warn you when a renewal has been failing — set certs.email in "+config.ConfigDir())
				rep.fixLast(manualFix)
			} else {
				ok("certificate contact email")
			}
		}
	}

	// ── Stopped service ports ────────────────────────────────────────────────
	// Surfaces the same diagnosis the UI shows on inactive service cards: if
	// a service unit is installed but stopped and its host port is already
	// bound by another process (a system-installed postgres, a stray docker
	// container, etc.), Start will fail with a generic bind error. List those
	// upfront so the user sees the conflict before clicking anything.
	section = "Stopped service ports"
	fmt.Fprintln(w, "\n[Stopped service ports]")
	{
		var stoppedUnits []string
		for _, name := range append([]string{}, knownServices()...) {
			unit := "servlo-" + name
			if !services.Mgr.ContainerUnitInstalled(unit) {
				continue
			}
			if services.Mgr.IsActive(unit) {
				continue
			}
			stoppedUnits = append(stoppedUnits, unit)
		}
		customs, _ := config.ListCustomServices()
		for _, svc := range customs {
			unit := "servlo-" + svc.Name
			if !services.Mgr.ContainerUnitInstalled(unit) {
				continue
			}
			if services.Mgr.IsActive(unit) {
				continue
			}
			stoppedUnits = append(stoppedUnits, unit)
		}

		if len(stoppedUnits) == 0 {
			ok("no stopped services to check")
		} else {
			ssOut := PortListOutput()
			conflictsFound := 0
			for _, unit := range stoppedUnits {
				for _, c := range CollectPortChecks([]string{unit}) {
					if PortInUseIn(c.Port, ssOut) {
						conflictsFound++
						warn(fmt.Sprintf("%s port %s", c.Label, c.Port),
							fmt.Sprintf("in use by another process, %s start may fail (find: %s)", c.Label, FindListenerCmd(c.Port)))
					}
				}
			}
			if conflictsFound == 0 {
				ok(fmt.Sprintf("%d stopped service(s), no port conflicts", len(stoppedUnits)))
			}
		}
	}

	// ── Containers & Images ──────────────────────────────────────────────────
	section = "Containers & Images"
	fmt.Fprintln(w, "\n[Containers & Images]")

	if !services.Mgr.ContainerUnitInstalled("servlo-nginx") {
		fail("servlo-nginx service", "not installed", "run: servlo install")
		rep.fixLast(autoFix(fixInstall, "", "install the servlo services (servlo install)"))
	} else {
		ok("servlo-nginx service installed")
	}

	phpVersions, _ := phpPkg.ListInstalled()
	if len(phpVersions) == 0 {
		warn("PHP versions", "none installed — run: servlo use 8.4")
	}
	for _, v := range phpVersions {
		short := strings.ReplaceAll(v, ".", "")
		image := "servlo-php" + short + "-fpm:local"
		// The base tag is the recipe hash, so an upstream PHP or Alpine fix
		// republishes it without moving any local hash. Nothing else on the
		// machine notices that the image has fallen behind it.
		exists := podman.ImageExists(image)
		base := (*podman.BaseImageStatus)(nil)
		if exists {
			base = podman.CheckBaseImageFreshness(v)
		}
		switch {
		case !exists:
			fail(fmt.Sprintf("PHP %s image", v), "missing", "servlo php:rebuild "+v)
			rep.fixLast(autoFix(fixPhpRebuild, v, "rebuild the PHP "+v+" image"))
		case base != nil && base.Stale:
			warn(fmt.Sprintf("PHP %s image", v), "its base image was refreshed upstream, run: servlo php:rebuild "+v)
			rep.fixLast(autoFix(fixPhpRebuild, v, "rebuild the PHP "+v+" image on the refreshed base"))
		default:
			ok(fmt.Sprintf("PHP %s image", v))
		}
	}

	if plan, planErr := cleanup.Inspect(cleanupScope(false)); planErr == nil && plan.ReclaimBytes() > 0 {
		info("Reclaimable disk", fmt.Sprintf("about %s (run: servlo cleanup)", humanSize(plan.ReclaimBytes())))
		rep.fixLast(autoFix(fixCleanup, "", "reclaim disk space (servlo cleanup)"))
	}

	// ── Container → Host Connectivity ────────────────────────────────────────
	// The PHP-FPM containers reach the host (host-side services)
	// via the host.containers.internal /etc/hosts entry. servlo writes that
	// IP based on a real reachability probe — TCP-connect each candidate
	// from inside servlo-nginx to servlo-panel's :7073. If no candidate works,
	// A host call times out silently with no error in the FPM logs other than
	// "Time-out connecting to debugging client" (issue #186 redux). This
	// check surfaces the failure so the user gets a real diagnosis.
	section = "Container → Host connectivity"
	fmt.Fprintln(w, "\n[Container → Host connectivity]")
	if !services.Mgr.IsActive("servlo-nginx") {
		warn("host reachability probe", "skipped — servlo-nginx not running (start servlo first)")
	} else if !services.Mgr.IsActive("servlo-panel") {
		warn("host reachability probe", "skipped — servlo-panel not running (the probe targets its :7073 listener)")
	} else if ip := podman.DetectHostGatewayIPProbeOnly(); ip != "" {
		ok(fmt.Sprintf("host reachable from containers (%s)", ip))
	} else {
		fail("host reachable from containers",
			"no candidate routed back to the host (inter-container → host calls will time out)",
			"check rootless podman / netavark / pasta routing; run: podman unshare --rootless-netns ip addr (expected: 169.254.1.2 on podman bridge or DNAT for it)")
	}

	if reg, regErr := config.LoadSites(); regErr == nil {
		for _, site := range reg.Sites {
			if site.Ignored || site.IsCustomContainer() || site.IsFrankenPHP() || site.IsHostProxy() {
				continue
			}
			hints := config.DetectFrankenPHPHints(site.Path)
			if len(hints) == 0 {
				continue
			}
			warn(fmt.Sprintf("site %s", site.Name),
				fmt.Sprintf("%s; switch with: servlo runtime frankenphp", hints[0].Reason))
		}
	}

	// ── Version Info ─────────────────────────────────────────────────────────
	section = "Version Info"
	fmt.Fprintln(w, "\n[Version Info]")

	info("servlo", version.String())

	if len(phpVersions) > 0 {
		info("PHP installed", strings.Join(phpVersions, ", "))
	} else {
		info("PHP installed", "(none)")
	}

	if cfg != nil {
		info("PHP default", cfg.PHP.DefaultVersion)
		info("Node default", cfg.Node.DefaultVersion)
	}

	if updateInfo, _ := servloUpdate.CachedUpdateCheck(version.Version); updateInfo != nil {
		warn("servlo update available", updateInfo.LatestVersion+" — run: servlo update, servlo whatsnew to see changes")
	} else {
		ok("servlo up to date")
	}

	// ── Summary ──────────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n══════════════════════════════════════════════")
	switch {
	case rep.Failures > 0 && rep.Warnings > 0:
		fmt.Fprintln(w, feedback.RedIf(useColor, fmt.Sprintf("%d failure(s), %d warning(s) found.", rep.Failures, rep.Warnings)))
	case rep.Failures > 0:
		fmt.Fprintln(w, feedback.RedIf(useColor, fmt.Sprintf("%d failure(s) found.", rep.Failures)))
	case rep.Warnings > 0:
		fmt.Fprintf(w, "%s  All critical checks passed.\n", feedback.AmberIf(useColor, fmt.Sprintf("%d warning(s) found.", rep.Warnings)))
	default:
		fmt.Fprintln(w, feedback.GreenIf(useColor, "All checks passed."))
	}

	return *rep, nil
}

// checkDirWritable returns an error if the directory doesn't exist or isn't writable.
func checkDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create: %v", err)
	}
	tmp, err := os.CreateTemp(dir, ".servlo-doctor-*")
	if err != nil {
		return fmt.Errorf("not writable: %v", err)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}

// PortInUse is implemented in doctor_linux.go.
//
// PortInUseIn checks whether the given TCP port appears in pre-fetched ss
// output. Used by checkPortConflicts in startstop.go for batch checks.
func PortInUseIn(port, output string) bool {
	return strings.Contains(output, ":"+port+" ")
}

// portFixHint renders the commands a port-strategy repair needs into one hint
// line. Servlo never runs them, so the hint has to carry the whole thing.
func portFixHint(cmds []string) string {
	if len(cmds) == 0 {
		return "see 'Port binding' in the architecture reference"
	}
	return "run: " + strings.Join(cmds, "  &&  ")
}
