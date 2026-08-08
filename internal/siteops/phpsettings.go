package siteops

import (
	"fmt"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/podman"
)

// PHPSettings is one site's PHP settings as the panel sends them. A struct
// rather than three arguments because they are saved together: a request
// carrying only one of them still says what the other two should be, so a
// partial save can never leave the site holding a value nobody chose.
type PHPSettings struct {
	MaxUploadMB         int
	MaxExecutionSeconds int
	MemoryLimitMB       int
}

// NginxSettings is a site's own nginx settings, saved the same way and for the
// same reason: the whole set every time.
type NginxSettings struct {
	StaticCacheDays int
	ResponseHeaders []config.ResponseHeader
}

// SetSiteNginxSettings saves a site's response headers and static-asset cache
// window and rewrites its vhost.
//
// Through the same commit as any other vhost write, so a header nginx will not
// accept rolls back to the previous config with nginx's own diagnostic instead
// of taking every site on the machine down.
func SetSiteNginxSettings(site *config.Site, s NginxSettings) error {
	updated := *site
	updated.StaticCacheDays = s.StaticCacheDays
	updated.ResponseHeaders = s.ResponseHeaders
	if err := updated.ValidateNginxSettings(); err != nil {
		return err
	}

	if err := config.AddSite(updated); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	*site = updated

	if err := RegenerateSiteVhost(site, site.PrimaryDomain()); err != nil {
		return err
	}
	if err := nginxReloadFn(); err != nil {
		return fmt.Errorf("reloading nginx: %w", err)
	}
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}
	return nil
}

// SetSitePHPSettings saves a site's PHP settings and makes them live.
//
// Each of the first two is one field that has to reach two files. Max upload
// size writes upload_max_filesize and post_max_size into the site's pool and
// client_max_body_size into its vhost; max execution time writes
// max_execution_time into the pool and the fastcgi read and send timeouts into
// the vhost. Both files are rewritten here, from the same saved site, so
// neither can be updated without the other (CLAUDE.md §3.4).
//
// The registry is written before either file, because both read the site back
// out of it.
func SetSitePHPSettings(site *config.Site, s PHPSettings) error {
	updated := *site
	updated.MaxUploadMB = s.MaxUploadMB
	updated.MaxExecutionSeconds = s.MaxExecutionSeconds
	updated.MemoryLimitMB = s.MemoryLimitMB
	if err := updated.ValidatePHPSettings(); err != nil {
		return err
	}

	if err := config.AddSite(updated); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	*site = updated

	// RegenerateSiteVhost syncs the pool first and writes the vhost from the
	// same site, which is what keeps the two halves of each pair together. No
	// domain changed, so the old primary is the current one.
	if err := RegenerateSiteVhost(site, site.PrimaryDomain()); err != nil {
		return err
	}
	if err := nginxReloadFn(); err != nil {
		return fmt.Errorf("reloading nginx: %w", err)
	}

	// Saving settings starts no systemd unit, so the shared hook would not
	// otherwise fire and an open dashboard would keep showing the old values
	// against a vhost already serving the new ones.
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}
	return nil
}
