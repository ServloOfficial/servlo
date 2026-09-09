package siteops

import (
	"os"
	"path/filepath"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/reqstats"
)

// IsParkedSite checks whether a site's path is inside one of the parked directories.
func IsParkedSite(sitePath string, parkedDirs []string) bool {
	parent := filepath.Dir(sitePath)
	for _, dir := range parkedDirs {
		expanded := os.ExpandEnv(dir)
		if home, err := os.UserHomeDir(); err == nil {
			if len(expanded) > 0 && expanded[0] == '~' {
				expanded = filepath.Join(home, expanded[1:])
			}
		}
		if parent == expanded {
			return true
		}
	}
	return false
}

// StopSiteWorkers, when set, stops all running workers for a site as part of
// UnlinkSiteCore. It is wired up by the cli package (which owns worker
// lifecycle) at init time, mirroring podman.AfterUnitChange. Without it, the
// parked-watcher unlink paths — which call UnlinkSiteCore directly —
// would leave a host-proxy site's always-restart dev-server worker (and any
// framework workers) running after the site is gone.
var StopSiteWorkers func(site *config.Site)

// RemoveSiteBackupSchedules takes a site's backup timer and its scheduled test
// restore off the machine as part of UnlinkSiteCore, for a site being removed
// outright. A hook for the same reason StopSiteWorkers is one, and one more
// besides: internal/backup imports this package, so the call cannot go the other
// way.
var RemoveSiteBackupSchedules func(siteName string)

// UnlinkSiteCore performs the shared unlink steps: stop workers, remove vhost,
// remove certs, update registry (ignore if parked, remove otherwise), update
// container hosts, and reload nginx.
func UnlinkSiteCore(site *config.Site, parkedDirs []string) error {
	if StopSiteWorkers != nil {
		StopSiteWorkers(site)
	}

	// A schedule outlives the site otherwise: the timer keeps firing a command
	// at a directory that is no longer served, and there is no longer a page in
	// the panel from which to notice or stop it.
	RemoveSiteCron(site)

	_ = nginx.RemoveVhost(site.PrimaryDomain())

	// Left behind, the pool keeps its master chdir'd into a directory that is
	// about to stop existing, and keeps a socket bound that the next site to
	// take this name would inherit.
	_ = RemoveFPMPool(site.Name)

	// Clean up the per-project custom container if this site uses one.
	// The image is kept so relinking is fast; use `servlo rebuild` to
	// force a fresh build.
	if site.IsCustomContainer() {
		_ = podman.StopUnit(podman.CustomContainerName(site.Name))
		podman.RemoveCustomContainer(site.Name)
		_ = podman.RemoveCustomContainerQuadlet(site.Name)
	}

	// Same cleanup for FrankenPHP sites: stop and remove the per-site
	// quadlet. The dunglas/frankenphp image is shared across all FrankenPHP
	// sites on this PHP version, so it stays in the local store.
	if site.IsFrankenPHP() {
		_ = podman.StopUnit(podman.FrankenPHPContainerName(site.Name))
		_ = podman.RemoveFrankenPHPQuadlet(site.Name)
	}

	// Custom-FPM PHP sites: stop the per-site FPM container and drop its
	// quadlet. The per-site image is kept so relinking is fast.
	if site.IsCustomFPM() {
		_ = podman.StopUnit(podman.CustomFPMContainerName(site.Name))
		_ = podman.RemoveCustomFPMQuadlet(site.Name)
	}

	if IsParkedSite(site.Path, parkedDirs) {
		_ = config.IgnoreSite(site.Name)
	} else {
		_ = config.RemoveSite(site.Name)
		_ = config.RemoveSiteFromWorkspaces(site.Name)
		// Both only on this branch. Unlinking a parked site is a tombstone
		// rather than a removal: linking the directory again brings the site
		// back, and it has to come back with its override and its certificate.
		// Everything else here is a unit or a generated file that relinking
		// rewrites for nothing, but a certificate costs a rate-limited exchange
		// with an authority that can refuse, and an override is something a
		// person typed.
		//
		// A site removed outright is not coming back, and both files are keyed
		// on the domain, so leaving them hands a stranger's nginx directives and
		// a live private key to whatever site next takes that domain.
		ForgetCustomNginx(site.PrimaryDomain())
		// And the backup pair, for the same reason plus one of its own. A
		// scheduled backup reads the site's files and its database rather than
		// running anything inside it, so unlike the cron units above it is not
		// broken by the site being unserved: a tombstoned site is still there to
		// back up. A site removed outright is not, so the timer fails nightly
		// and then starts backing up whatever site next takes the name, on a
		// schedule its operator never set.
		if RemoveSiteBackupSchedules != nil {
			RemoveSiteBackupSchedules(site.Name)
		}
		// Not conditional on Secured either. The flag says what the vhost
		// serves, and the files are named for the domain either way: an issuance
		// whose vhost step failed leaves a key behind a site that never read as
		// secured.
		certs.ForgetSite(site.PrimaryDomain())
		// And a staging site's password file, which is named for the domain
		// like the rest of them. Two comments in internal/staging said site
		// removal took care of this and nothing here ever did, so every staging
		// site ever unlinked left its hash on disk at 0644.
		nginx.RemoveHtpasswd(site.PrimaryDomain())
	}

	forgetSiteState(site.Name)

	_ = podman.WriteContainerHosts()
	_ = podman.RewriteFPMQuadlets()

	if err := nginx.Reload(); err != nil {
		return err
	}

	// See FinishLink: unlinking doesn't start/stop a systemd unit, so
	// the shared hook wouldn't otherwise fire. Notify explicitly.
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}
	return nil
}

// forgetSiteState drops the per-site request-timing state the watcher writes,
// which the rest of the unlink path leaves behind: the durable request store,
// the snapshot file, and (via the control socket) the running watcher's
// in-memory copy so it stops re-emitting the site. All best-effort: a site with no recorded state or a down watcher just no-ops.
func forgetSiteState(name string) {
	_ = reqstats.RemoveSite(config.RequestStatsFile(), name)
	if _, err := os.Stat(config.RequestStatsDB()); err == nil {
		if st, err := reqstats.OpenShared(config.RequestStatsDB()); err == nil {
			_, _ = st.DeleteSite(name)
		}
	}
}
