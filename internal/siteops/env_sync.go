package siteops

import (
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
)

// SyncEnvIfPrimaryChanged updates APP_URL and the VITE_REVERB_* keys in the
// site's project .env, but only when the primary domain has actually changed
// since oldPrimary.
func SyncEnvIfPrimaryChanged(site *config.Site, oldPrimary string) error {
	newPrimary := site.PrimaryDomain()
	if newPrimary == oldPrimary {
		return nil
	}
	if err := envfile.SyncPrimaryDomain(site.Path, newPrimary, site.Secured); err != nil {
		return err
	}
	return nil
}
