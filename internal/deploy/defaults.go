package deploy

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
	gitpkg "github.com/realrashid/servlo/internal/git"
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

// RunScript runs a site's deploy script with the site as its working
// directory.
//
// Through `sh -c` rather than by writing the script somewhere executable and
// running it: the script is already a file, but running it in place would mean
// its permissions and its shebang decide how it executes, and an operator who
// saved one without a shebang would get a confusing failure rather than the
// shell they expected.
func RunScript(dir, script string, out io.Writer) error {
	// -e so the deploy stops at the first command that fails. Without it a
	// composer install that could not reach the network is followed by a
	// migration against half-installed code, and the deploy reports success.
	cmd := exec.Command("sh", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
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
