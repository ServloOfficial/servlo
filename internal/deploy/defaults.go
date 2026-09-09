package deploy

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ServloOfficial/servlo/internal/buildscope"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbdump"
	"github.com/ServloOfficial/servlo/internal/envfile"
	gitpkg "github.com/ServloOfficial/servlo/internal/git"
	"github.com/ServloOfficial/servlo/internal/node"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/serviceops"
	"github.com/ServloOfficial/servlo/internal/siteops"
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
		BuildWarning: func(script string) string {
			return buildscope.Warning(buildscope.HostTotalRAM(), script)
		},
		Migrates: siteops.DeployScriptMigrates,
		Script:   deployScript,
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

// wrapScopeFn confines the build, indirected so a test does not need a systemd
// user session.
var wrapScopeFn = buildscope.Wrap

// remoteDump is how a database servlo does not host is read. Indirected so a
// test can exercise the pre-deploy backup without a managed database and the
// engine's client binaries to reach it with.
var remoteDump = dbdump.Dump

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

	plain, err := nodeScriptCommand(dir, args)
	if err != nil {
		return err
	}

	// The scope goes outside the Node wrapper, not inside it. Whatever fnm
	// spawns has to be in the cgroup too, or the build escapes the ceiling
	// through the very process that does the building.
	limit := buildscope.HostLimit()
	cmd := wrapScopeFn(plain, limit, buildscope.UnitName(filepath.Base(dir)))
	// Whether this machine could actually confine the build. It decides how a
	// killed build is read afterwards: inside a scope the kill is the ceiling,
	// outside one the same signals mean somebody stopped the deploy by hand, and
	// telling them they ran out of memory would send them after the wrong thing.
	confined := cmd != plain
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		if confined && buildscope.OutOfMemory(err) {
			// Exit 137 tells an operator nothing. This is the one build failure
			// where the output is empty and the fix is not another try.
			return fmt.Errorf(
				"the build ran out of memory and was stopped at its %dMB ceiling, so nothing else on this server was affected: "+
					"give the build less to do, or move this site to a larger machine",
				limit/buildscope.MB)
		}
		return err
	}
	return nil
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
	host := strings.TrimSpace(vals["DB_HOST"])
	service := strings.TrimPrefix(host, "servlo-")
	database := strings.TrimSpace(vals["DB_DATABASE"])
	if host == "" || database == "" {
		return "", fmt.Errorf(
			"this deploy runs a migration but servlo cannot tell which database to back up: " +
				"set DB_HOST and DB_DATABASE in the site's .env, or take the migration out of the deploy script")
	}

	name := snapshotName(site)
	meta := serviceops.SnapshotMeta{Site: site.Name, GitBranch: gitpkg.MainBranch(site.Path)}

	// A database servlo runs is dumped inside its own container, which is both
	// faster and the only path that needs no credentials of its own.
	if serviceops.SnapshotSupported(service, false) {
		target := serviceops.SnapshotTarget{
			Service:  service,
			Family:   config.FamilyOfName(service),
			Database: database,
		}
		snap, err := serviceops.CreateSnapshot(target, name, meta, nil)
		if err != nil {
			return "", err
		}
		return snap.Name, nil
	}

	// Otherwise the site is on a database servlo does not host, which has no
	// container to dump inside. It is reached the way the rest of servlo reaches
	// one, through the site's named connection, and the dump is filed in the
	// same store. Without this a site on a managed database could not run a
	// migrating deploy at all: the guarantee that a migration is preceded by a
	// backup would fail on the backup, and the deploy would stop every time.
	conn, err := dbconn.Named(site.Database)
	if err != nil || conn.Local() {
		return "", fmt.Errorf(
			"this deploy runs a migration and %s is not a database servlo can back up: "+
				"add it as a connection with `servlo db:connection add` and point the site at it, "+
				"or take the migration out of the deploy script", host)
	}
	return snapshotRemote(conn, database, name, meta)
}

// snapshotRemote dumps a database servlo does not host into the snapshot store.
//
// The dump is streamed rather than spooled: a production database is exactly
// the thing that does not fit in a temporary file nobody sized for it, and the
// deploy is already waiting on the dump either way.
func snapshotRemote(conn dbconn.Connection, database, name string, meta serviceops.SnapshotMeta) (string, error) {
	target := serviceops.SnapshotTarget{
		Service:  conn.Name,
		Family:   conn.Family,
		Database: database,
	}
	r, w := io.Pipe()
	go func() { w.CloseWithError(remoteDump(conn, database, w)) }()
	defer r.Close()
	snap, err := serviceops.StoreSnapshot(target, name, meta, r)
	if err != nil {
		return "", err
	}
	return snap.Name, nil
}

// snapshotName says what the backup is and which site it came from, so the one
// an operator wants after a bad deploy is findable in a list months later. The
// store stamps it with the time, which is why there is no clock here: naming it
// twice produced predeploy-shop-<time>-<time> on disk while the deploy reported
// the half of it without the store's stamp, so the name an operator was told to
// look for was not the name of anything.
func snapshotName(site *config.Site) string {
	return "predeploy-" + site.Name
}
