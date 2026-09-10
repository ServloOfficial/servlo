package siteops

import (
	"errors"
	"fmt"
	"os"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/fpmpool"
	"github.com/ServloOfficial/servlo/internal/podman"
)

// ReloadFPMPoolsFn is podman.ReloadFPMPools, indirected so a caller's tests can
// watch what gets signalled without a container to signal. Exported for the
// same reason NginxTestFn and NginxReloadFn are: the panel drives this path,
// and its tests cannot start a container either.
var ReloadFPMPoolsFn = podman.ReloadFPMPools

// SyncFPMPool gives a site its own PHP-FPM pool, in the directory belonging to
// the FPM container that serves it, and tells that container's master to
// re-read it.
//
// The pool is written before the vhost, because the vhost only points at the
// site's socket once the pool file exists: written the other way round, a new
// site would stay on the shared container until something else happened to
// regenerate its vhost.
//
// Any pool left in another container's directory is removed first. A site that
// changes PHP version moves between containers, and the pool it left behind
// would have the old master still binding the socket the new one needs.
// SyncSitePHPSettings puts a site's PHP settings wherever that site's runtime
// reads them from.
//
// A shared-FPM site reads them out of its pool. A site running its own container
// has no pool and reads them out of an ini that container mounts. Both halves of
// every pair in CLAUDE.md §3.4 move on one save either way, which is the whole
// point of the pairs: the vhost side is written for every runtime, so a runtime
// whose PHP side went nowhere was a site where nginx allowed an upload PHP then
// refused.
func SyncSitePHPSettings(site config.Site) error {
	if servedByFPM(site) {
		if err := SyncFPMPool(site); err != nil {
			return fmt.Errorf("writing the site's PHP-FPM pool: %w", err)
		}
		return nil
	}
	// The pool may still exist from before the site changed runtime, and its
	// old master would go on binding the socket.
	if err := RemoveFPMPool(site.Name); err != nil {
		return err
	}
	if !site.IsFrankenPHP() {
		// A custom container and a host proxy run something servlo did not
		// build. There is no file of servlo's that their PHP, if they have any,
		// would read.
		return nil
	}
	if err := site.ValidatePHPSettings(); err != nil {
		return err
	}
	changed, err := writeManagedSiteIni(site)
	if err != nil {
		return fmt.Errorf("writing the site's PHP settings: %w", err)
	}
	if changed {
		RestartSiteRuntimeFn(site)
	}
	return nil
}

// RestartSiteRuntimeFn makes a written setting live on a site that runs its own
// container, indirected so a test does not need systemd. A setting written and
// not picked up is the same half-applied state the pairs exist to prevent.
//
// The unit is rewritten before the restart, not only restarted. A site created
// before servlo mounted this file has a unit that does not mount it, and writing
// the file without the mount is a setting that goes nowhere just as surely as
// not writing it at all.
var RestartSiteRuntimeFn = func(site config.Site) {
	entrypoint, env := site.FrankenPHPQuadletSpec()
	if _, err := podman.WriteFrankenPHPQuadletDiff(site.Name, site.Path, site.PHPVersion, entrypoint, env); err != nil {
		return
	}
	_ = podman.DaemonReloadFn()
	_ = podman.RestartUnit(podman.FrankenPHPContainerName(site.Name))
}

func SyncFPMPool(site config.Site) error {
	if err := RemoveFPMPool(site.Name); err != nil {
		return err
	}
	if !servedByFPM(site) || !fpmpool.UsablePool(config.FPMSocketDir(), site.Name) {
		return nil
	}

	container := podman.FPMContainerName(site, site.PHPVersion)
	if err := site.ValidatePHPSettings(); err != nil {
		return err
	}
	if _, err := fpmpool.Write(config.FPMPoolDir(container), fpmpool.Settings{
		Site:      site.Name,
		Root:      site.Path,
		SocketDir: config.FPMSocketDir(),
		// The PHP half of each pair. nginx's half of the same two fields is
		// written into the vhost from the same site, see nginx.VhostData.
		MaxUploadMB:         site.MaxUploadMB,
		MaxExecutionSeconds: site.MaxExecutionSeconds,
		MemoryLimitMB:       site.MemoryLimitMB,
	}); err != nil {
		return err
	}
	if err := os.MkdirAll(config.FPMSocketDir(), 0o755); err != nil {
		return err
	}
	return ReloadFPMPoolsFn(container)
}

// RemoveFPMPool drops a site's pool from every FPM container's directory and
// reloads the ones that had it. Every directory rather than the one the site
// currently uses, because a site whose PHP version changed has a pool in the
// container it came from as well.
func RemoveFPMPool(name string) error {
	if !fpmpool.UsableHandle(name) {
		return nil
	}
	entries, err := os.ReadDir(config.FPMPoolRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := config.FPMPoolDir(e.Name())
		if _, err := os.Stat(fpmpool.Path(dir, name)); err != nil {
			continue
		}
		if err := fpmpool.Remove(dir, name); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := ReloadFPMPoolsFn(e.Name()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// servedByFPM reports whether the site's PHP requests reach an FPM container at
// all. A FrankenPHP site, a custom-container site and a host-proxy site are all
// reverse-proxied, so a pool for one would listen on a socket nothing asks for.
func servedByFPM(site config.Site) bool {
	return !site.IsFrankenPHP() && !site.IsCustomContainer() && !site.IsHostProxy()
}
