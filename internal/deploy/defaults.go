package deploy

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
	gitpkg "github.com/realrashid/servlo/internal/git"
	"github.com/realrashid/servlo/internal/node"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/serviceops"
	"github.com/realrashid/servlo/internal/siteops"
)

// The real implementations of everything Options declares. Split out so the
// sequence in deploy.go can be read, and tested, without any of this.

// Defaults returns the Options a real deploy runs with. A caller supplies the
// site and where the output goes, and overrides nothing else.
func Defaults(site *config.Site, out io.Writer) Options {
	return Options{
		Site:      site,
		Out:       out,
		Git:       gitpkg.Run,
		Head:      Head,
		Snapshot:  Snapshot,
		RunScript: RunScript,
		Reload:    Reload,
		Keep:      Keep,
		Excludes:  siteops.DeployExcludes,
		Migrates:  siteops.DeployScriptMigrates,
		Script:    deployScript,
	}
}

func deployScript(site *config.Site) (string, error) {
	content, err := siteops.ReadDeployScript(site)
	if err != nil {
		return "", err
	}
	return content.Body, nil
}

// Head is the commit a site is currently on.
func Head(dir string) (string, error) {
	out, err := gitpkg.Output(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// The Node manager, reached through variables so a test can run a deploy
// script without fnm on the machine.
var (
	nodeActive        = func() node.Manager { return node.Active() }
	nodeDetectVersion = node.DetectVersion
)

// RunScript runs a site's deploy script with the site as its working directory,
// under the site's own Node version.
//
// Through `sh -c` rather than by writing the script somewhere executable and
// running it: the script is already a file, but running it in place would mean
// its permissions and its shebang decide how it executes, and an operator who
// saved one without a shebang would get a confusing failure rather than the
// shell they expected.
//
// The Node version is the site's, not the daemon's. This is the one place a
// site's assets are actually built, and a deploy that ran `npm run build` under
// whatever Node the panel happens to have would ship a bundle from the wrong
// toolchain to a site that carefully pinned one. The whole script goes under
// the version rather than each command, because a script is several commands
// and the second one needs the same Node as the first.
func RunScript(dir, script string, out io.Writer) error {
	// -e so the deploy stops at the first command that fails. Without it a
	// composer install that could not reach the network is followed by a
	// migration against half-installed code, and the deploy reports success.
	args := []string{"-e", "-c", script}

	cmd, err := nodeScriptCommand(dir, args)
	if err != nil {
		return err
	}
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// nodeScriptCommand builds the shell command, under the site's Node version
// when there is one to use.
func nodeScriptCommand(dir string, args []string) (*exec.Cmd, error) {
	mgr := nodeActive()
	if mgr == nil || !mgr.Available() {
		// No Node on this machine at all. Most WordPress sites never run a
		// build, and a PHP-only script must not need a Node manager to exist.
		return exec.Command("sh", args...), nil
	}

	version, _ := nodeDetectVersion(dir)
	if version == "" || version == "default" {
		if !mgr.HasDefault() {
			return exec.Command("sh", args...), nil
		}
		return mgr.Command("default", "sh", args), nil
	}

	// A pinned version that is not installed is refused rather than silently
	// swapped for another. Installing one here would mean a deploy quietly
	// downloading a toolchain, which is minutes of surprise in the middle of an
	// operation somebody is watching.
	if !slices.Contains(mgr.List(), version) {
		return nil, fmt.Errorf(
			"this site builds with Node %s and it is not installed: run `servlo node:install %s` and deploy again",
			version, version)
	}
	return mgr.Command(version, "sh", args), nil
}

// Reload makes a site's new code live, gracefully.
//
// SIGUSR2 to the FPM master rather than a restart: the master re-reads its
// configuration, starts new workers and lets the running ones finish what they
// are doing. A deploy that dropped every in-flight request on every site
// sharing the container would be a worse outcome than the one it replaced.
//
// This is also the step that matters most on a production site. OPcache runs
// with validate_timestamps off, so PHP keeps serving the bytecode it already
// has: without the reload, the pull and the script change the files on disk and
// nothing visitors see.
func Reload(site *config.Site) error {
	return podman.ReloadFPMPools(podman.FPMContainerName(*site, site.PHPVersion))
}

// Snapshot takes the pre-deploy database backup.
//
// The database comes from the site's own env, the same way every other part of
// servlo finds it. When it cannot be found, this returns an error rather than
// nothing, and the deploy stops: a migration running without the backup that
// was supposed to precede it is the one outcome this whole path exists to
// prevent, and quietly skipping it would be worse than refusing.
func Snapshot(site *config.Site) (string, error) {
	vals := envfile.ReadValues(filepath.Join(site.Path, ".env"))
	service := strings.TrimPrefix(strings.TrimSpace(vals["DB_HOST"]), "servlo-")
	database := strings.TrimSpace(vals["DB_DATABASE"])
	if service == "" || database == "" {
		return "", fmt.Errorf(
			"this deploy runs a migration but servlo cannot tell which database to back up: " +
				"set DB_HOST and DB_DATABASE in the site's .env, or take the migration out of the deploy script")
	}

	name := snapshotName(site)
	target := serviceops.SnapshotTarget{
		Service:  service,
		Family:   config.FamilyOfName(service),
		Database: database,
	}
	meta := serviceops.SnapshotMeta{Site: site.Name, GitBranch: gitpkg.MainBranch(site.Path)}
	if _, err := serviceops.CreateSnapshot(target, name, meta, nil); err != nil {
		return "", err
	}
	return name, nil
}

// snapshotName says what the backup is and when, so the one an operator wants
// after a bad deploy is findable in a list months later.
func snapshotName(site *config.Site) string {
	return fmt.Sprintf("predeploy-%s-%s", site.Name, time.Now().UTC().Format("20060102-150405"))
}
