package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dns"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/services"
	"github.com/spf13/cobra"
)

// NewUninstallCmd returns the uninstall command.
func NewUninstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Servlo and all its components",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUninstall(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompts")
	return cmd
}

func runUninstall(force bool) error {
	feedback.Begin()
	feedback.Line("uninstalling servlo")

	if !force {
		if !feedback.Confirm("This will stop all containers and remove servlo. Continue?", false) {
			feedback.Line("aborted")
			return nil
		}
	}

	// Ask about data removal up front — the StepRunner puts stdin into raw
	// mode and its reader goroutine would consume bytes meant for this prompt.
	removeData := force || confirmRemoveData()

	// Global npm packages the npm shim captured into servlo's prefix would
	// silently vanish with the data dir — nobody expects uninstalling a dev
	// tool to take their globals with it. Offer to move them back to the
	// user's own npm before anything is deleted.
	var strandedGlobals []string
	var reinstallNPM string
	if removeData {
		strandedGlobals = nodeGlobalPackages(config.NodeGlobalDir())
		if len(strandedGlobals) > 0 && !force {
			if npm := systemNPMPath(); npm != "" && feedback.Confirm(
				fmt.Sprintf("Reinstall %d global npm package(s) (%s) with your own npm?",
					len(strandedGlobals), formatNodeGlobalsNote(strandedGlobals)), true) {
				reinstallNPM = npm
			}
		}
	}

	removeMkcertCA := force || confirmRemoveMkcertCA()
	purgeImages := force || confirmPurgeServloImages()

	// DNS teardown runs outside the step runner because it may prompt for sudo;
	// the lock glyph warns that the password prompt below is expected.
	feedback.Sudo("Removing DNS configuration")
	dns.Teardown()

	step("Stopping containers and services")
	{
		// Use the service manager so this works on both Linux (systemd/quadlet)
		// units.
		seen := map[string]bool{}
		for _, unit := range services.Mgr.ListContainerUnits("servlo-*") {
			seen[unit] = true
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				_ = podman.StopUnit(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		for _, unit := range services.Mgr.ListServiceUnits("servlo-*") {
			if seen[unit] {
				continue
			}
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				_ = podman.StopUnit(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		for _, unit := range services.Mgr.ListTimerUnits("servlo-*") {
			_ = podman.StopUnit(unit)
			_ = services.Mgr.Disable(unit)
		}
	}
	ok()

	step("Removing service units")
	{
		seen := map[string]bool{}
		for _, unit := range services.Mgr.ListContainerUnits("servlo-*") {
			seen[unit] = true
			_ = services.Mgr.RemoveContainerUnit(unit)
		}
		for _, unit := range services.Mgr.ListServiceUnits("servlo-*") {
			if seen[unit] {
				continue
			}
			_ = services.Mgr.RemoveServiceUnit(unit)
		}
		for _, unit := range services.Mgr.ListTimerUnits("servlo-*") {
			_ = services.Mgr.RemoveTimerUnit(strings.TrimSuffix(unit, ".timer"))
		}
	}
	ok()

	step("Reloading service manager")
	_ = podman.DaemonReloadFn()
	ok()

	step("Removing servlo Podman network")
	_ = podman.RemoveNetwork("servlo")
	ok()

	if purgeImages {
		feedback.Line("purging servlo-built container images")
		removeServloImages()
	}

	if removeMkcertCA {
		feedback.Sudo("Uninstalling mkcert CA from system trust stores")
		cmd := exec.Command(certs.MkcertPath(), "-uninstall")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		// mkcert only removes the anchor it wrote itself, so the one servlo
		// installed as root has to go separately or it stays trusted for good.
		removeSystemTrustAnchor()
	}

	step("Removing shell PATH entry")
	removeShellEntry()
	ok()

	step("Removing servlo binaries")
	// A binary someone else owns is left where it is: deleting a file out of a
	// package's file list leaves that manager believing servlo is still installed.
	if self, err := selfPath(); err == nil && isSystemPackageManaged(self) {
		fmt.Println(feedback.Dim("kept, package-managed"))
		feedback.Note("remove the binaries with your package manager, e.g. " + packageManagerRemoveHint(self))
	} else {
		if err == nil {
			removeInstalledBinaries(self)
		}
		ok()
	}

	if removeData {
		if reinstallNPM != "" {
			feedback.Line("reinstalling global npm packages with your own npm")
			if err := reinstallNodeGlobals(reinstallNPM, strandedGlobals); err != nil {
				feedback.Warn("npm install -g failed: %v", err)
				feedback.Note("reinstall them yourself with: npm install -g " + strings.Join(strandedGlobals, " "))
			}
		} else if len(strandedGlobals) > 0 {
			feedback.Note("global npm packages removed with servlo: " + formatNodeGlobalsNote(strandedGlobals))
			feedback.Note("reinstall them with: npm install -g " + strings.Join(strandedGlobals, " "))
		}
		step("Removing config and data directories")
		os.RemoveAll(config.ConfigDir())
		os.RemoveAll(config.DataDir())
		ok()
	} else {
		feedback.Note("config kept at " + config.ConfigDir())
		feedback.Note("data kept at " + config.DataDir())
	}

	feedback.Done("servlo uninstalled")
	return nil
}

func confirmRemoveMkcertCA() bool {
	return feedback.Confirm("Uninstall mkcert CA from system trust stores?", false)
}

func confirmPurgeServloImages() bool {
	return feedback.Confirm("Purge servlo-built container images (servlo-php*-fpm, servlo-custom-*, servlo-dnsmasq)? Databases and app files are unaffected.", false)
}

// removeServloImages removes locally-built servlo images. Upstream pulls
// (mysql/redis/postgres/etc.) are left alone since they're expensive to
// re-pull and not servlo-owned.
func removeServloImages() {
	out, err := podman.Cmd("images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		feedback.Warn("listing images: %v", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isServloBuiltImage(line) {
			continue
		}
		if err := podman.Cmd("image", "rm", "-f", line).Run(); err != nil {
			feedback.Warn("removing %s: %v", line, err)
			continue
		}
		feedback.Note("removed " + line)
	}
}

// isServloBuiltImage matches the locally-built tags servlo owns.
func isServloBuiltImage(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "servlo-php") && strings.HasSuffix(ref, "-fpm:local"):
		return true
	case strings.HasPrefix(ref, "servlo-custom-") && strings.HasSuffix(ref, ":local"):
		return true
	case ref == "servlo-dnsmasq:local":
		return true
	}
	return false
}

func confirmRemoveData() bool {
	return feedback.Confirm("Remove all config and data (~/.config/servlo, ~/.local/share/servlo)?", false)
}

// shellRCMarkers lists every marker comment servlo's install pipelines
// ever wrote into a user shell rc, paired with the number of follow-up
// lines belonging to that block. Each entry is a separate writer:
//
//   - "# Added by Servlo installer" → install.sh; 1 trailing line
//     (`export PATH=...` or fish `fish_add_path …`)
//   - "# Servlo"                    → install.go appendShellRC; 1 trailing
//     line (`export PATH=...`)
//   - "# Servlo completions"        → install.go ensureZshFpath; 2 trailing
//     lines (`fpath=(...)` + `autoload -Uz compinit && compinit`)
//
// uninstall used to match only the first marker, which left the other
// two blocks behind on every install path that went through `servlo
// install` (which is most of them). User-visible: `# Servlo … export
// PATH …` lingering in `~/.zshrc` after a clean uninstall.
var shellRCMarkers = []struct {
	marker    string
	skipAfter int
}{
	{"# Added by Servlo installer", 1},
	{"# Servlo completions", 2}, // must be before "# Servlo" — longer prefix wins
	{"# Servlo", 1},
}

// removeInstalledBinaries deletes the servlo binary the installer put in place.
func removeInstalledBinaries(self string) {
	os.Remove(self) //nolint:errcheck
}

func removeShellEntry() {
	home, _ := os.UserHomeDir()

	candidates := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".config", "fish", "conf.d", "servlo.fish"),
	}

	for _, rc := range candidates {
		for _, m := range shellRCMarkers {
			removeMarkedBlock(rc, m.marker, m.skipAfter)
		}
		// The fish path is servlo-dedicated — if our removals left only
		// whitespace behind, delete the file rather than leave an empty
		// conf.d entry that fish still sources on every shell start.
		if strings.HasSuffix(rc, "servlo.fish") {
			if data, err := os.ReadFile(rc); err == nil && strings.TrimSpace(string(data)) == "" {
				os.Remove(rc) //nolint:errcheck
			}
		}
	}
}

// removeMarkedBlock removes the marker line plus skipAfter follow-up
// lines. The marker match is exact (after TrimSpace) so a comment that
// happens to contain "# Servlo" as a substring (e.g. "# Servlo-related
// notes") isn't touched.
func removeMarkedBlock(path, marker string, skipAfter int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	skip := 0
	for _, line := range lines {
		if skip > 0 {
			skip--
			continue
		}
		if strings.TrimSpace(line) == marker {
			skip = skipAfter
			continue
		}
		out = append(out, line)
	}

	// Only rewrite if something changed
	result := strings.Join(out, "\n")
	if result != string(data) {
		os.WriteFile(path, []byte(result), 0644) //nolint:errcheck
	}
}
