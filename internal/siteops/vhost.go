package siteops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
	"github.com/ServloOfficial/servlo/internal/podman"
)

// RegenerateSiteVhost regenerates the nginx vhost for a site after domain changes.
// If the primary domain changed, the old vhost file is removed. For secured sites
// the SSL vhost is generated and renamed to the main .conf path. Every caller that
// changes a site's domains comes through here, so it is also where a running dev
// server is realigned with them.
func RegenerateSiteVhost(site *config.Site, oldPrimary string) error {
	newPrimary := site.PrimaryDomain()

	// The vhost reads the pool to decide where to send PHP, so the pool is
	// brought up to date first. This is also the path a site takes when it
	// changes PHP version, which moves its pool between containers.
	if err := SyncFPMPool(*site); err != nil {
		return fmt.Errorf("writing the site's PHP-FPM pool: %w", err)
	}

	if oldPrimary != newPrimary {
		_ = nginx.RemoveVhost(oldPrimary)
		if err := MoveCustomNginxConfig(oldPrimary, newPrimary); err != nil {
			fmt.Fprintf(os.Stderr, "servlo: migrating custom nginx override to %s: %v\n", newPrimary, err)
		}
	}

	if site.IsHostProxy() {
		if site.Secured {
			if err := nginx.GenerateHostProxySSLVhost(*site); err != nil {
				return fmt.Errorf("generating host-proxy SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), newPrimary+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), newPrimary+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing host-proxy SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateHostProxyVhost(*site); err != nil {
				return fmt.Errorf("generating host-proxy vhost: %w", err)
			}
		}
	} else if site.IsCustomContainer() {
		if site.Secured {
			if err := nginx.GenerateCustomSSLVhost(*site); err != nil {
				return fmt.Errorf("generating custom SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), newPrimary+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), newPrimary+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing custom SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateCustomVhost(*site); err != nil {
				return fmt.Errorf("generating custom vhost: %w", err)
			}
		}
	} else if site.Secured {
		if err := nginx.GenerateSSLVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("generating SSL vhost: %w", err)
		}
		sslConf := filepath.Join(config.NginxConfD(), newPrimary+"-ssl.conf")
		mainConf := filepath.Join(config.NginxConfD(), newPrimary+".conf")
		_ = os.Remove(mainConf)
		if err := os.Rename(sslConf, mainConf); err != nil {
			return fmt.Errorf("installing SSL vhost: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("generating vhost: %w", err)
		}
	}
	// A dev server serving under this vhost has the site's domains baked into the
	// config it was started with, so it follows the vhost that just moved.
	RefreshDevServers(site)
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}
	return nil
}

// MoveCustomNginxConfig follows a site's hand-authored nginx override across a
// primary-domain rename. The snippet lives at custom.d/{primary}.conf and the
// generated vhost includes it by name, so without this it is orphaned and the
// renamed site silently loses its custom config. Timestamped backups in
// custom.d.bkp/ are keyed the same way and moved too so the UI restore dropdown
// keeps working. Missing files are not an error; renames far outnumber edits.
func MoveCustomNginxConfig(oldPrimary, newPrimary string) error {
	if oldPrimary == newPrimary {
		return nil
	}
	live := config.NginxCustomD()
	// The main override is keyed solely by primary domain, so any file already
	// at the new name can only be a stale orphan from a prior rename (active
	// sites cannot share a primary), hence clobber=true.
	if err := moveFile(
		filepath.Join(live, oldPrimary+".conf"),
		filepath.Join(live, newPrimary+".conf"),
		true,
	); err != nil {
		return err
	}
	var firstErr error
	bkp := config.NginxCustomDBkp()
	entries, err := os.ReadDir(bkp)
	if err != nil {
		if os.IsNotExist(err) {
			return firstErr
		}
		return err
	}
	// Never clobber, so a same-second collision can't destroy recoverable
	// history.
	mainPrefix := oldPrimary + ".conf.bkp."
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, mainPrefix) {
			continue
		}
		newName := newPrimary + ".conf.bkp." + strings.TrimPrefix(name, mainPrefix)
		if err := moveFile(filepath.Join(bkp, name), filepath.Join(bkp, newName), false); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// moveFile renames src to dst when src exists. A missing src is a no-op. When
// clobber is false an existing dst is left untouched (src stays put) so no data
// is destroyed; when true an existing dst is replaced.
func moveFile(src, dst string, clobber bool) error {
	if src == dst {
		return nil
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !clobber {
		if _, err := os.Stat(dst); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(src, dst)
}
