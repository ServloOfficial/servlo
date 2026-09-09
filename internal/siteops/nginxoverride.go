package siteops

import (
	"os"
	"path/filepath"

	"github.com/ServloOfficial/servlo/internal/cfgedit"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
)

// NginxTestFn / NginxReloadFn are indirections so tests can stub the
// podman-bound `nginx -t` and reload. The global-nginx editor reuses these so
// site and global saves share one validator/reload pair. Defaults are real.
var (
	NginxTestFn   = nginx.Test
	NginxReloadFn = nginx.Reload
)

// nginxSiteTemplate seeds the editor when no override exists yet. Everything
// is commented so the file is an inert no-op until the user opts in.
const nginxSiteTemplate = `# Servlo per-site nginx overrides.
#
# Included at the end of this site's server { } block. Servlo never overwrites
# this file, so edits survive vhost regeneration and ` + "`servlo update`" + `. Add
# directives valid inside a server block, then save to reload nginx.

# client_max_body_size 100m;
# location /ws { proxy_pass http://127.0.0.1:6001; proxy_http_version 1.1; }
`

// CustomNginxPath is the on-disk path of a domain's custom override.
func CustomNginxPath(domain string) string {
	return filepath.Join(config.NginxCustomD(), domain+".conf")
}

// nginxFile builds the cfgedit.File for a domain's custom override. Backups and
// write-staging live in custom.d.bkp/, kept off the custom.d/*.conf* glob.
func nginxFile(domain string) cfgedit.File {
	return cfgedit.File{
		Path:     CustomNginxPath(domain),
		BkpDir:   config.NginxCustomDBkp(),
		BkpName:  domain + ".conf",
		Template: nginxSiteTemplate,
	}
}

// nginxValidate runs `nginx -t` (via the stubbable indirection) so cfgedit can
// pre-flight a save without importing nginx itself.
func nginxValidate(string) (string, error) { return NginxTestFn() }

// ReadCustomNginx returns the saved override, or the seeded template
// (Exists=false) when nothing is on disk yet.
func ReadCustomNginx(domain string) (cfgedit.Content, error) {
	return nginxFile(domain).Read()
}

// SaveCustomNginx writes, validates with `nginx -t`, rolls back on a failure
// that names our file, and reloads nginx.
func SaveCustomNginx(domain, content string, backup bool) (cfgedit.SaveResult, error) {
	return nginxFile(domain).Save(content, cfgedit.SaveOpts{
		Backup:   backup,
		Validate: nginxValidate,
		Owns:     cfgedit.MentionsFile,
		Apply:    func() error { return NginxReloadFn() },
	})
}

// ResetCustomNginx deletes the override and reloads nginx. Backups are kept.
func ResetCustomNginx(domain string) error {
	return nginxFile(domain).Reset(func() error { return NginxReloadFn() })
}

// ForgetCustomNginx deletes a domain's override and every backup of it, for a
// site that is being removed rather than edited.
//
// Both are keyed on the primary domain, and the generated vhost includes the
// file by name, so one left behind is not inert for long: the next site on that
// domain comes up with a stranger's hand-written nginx directives applied to it,
// and a restore dropdown offering that site's history.
func ForgetCustomNginx(domain string) {
	f := nginxFile(domain)
	os.Remove(f.Path) //nolint:errcheck — a site with no override is the common case
	backups, err := f.ListBackups()
	if err != nil {
		return
	}
	for _, b := range backups {
		os.Remove(filepath.Join(config.NginxCustomDBkp(), b.Name)) //nolint:errcheck
	}
}

// ListCustomNginxBackups returns a domain's override backups, newest first.
func ListCustomNginxBackups(domain string) ([]cfgedit.Backup, error) {
	return nginxFile(domain).ListBackups()
}

// ReadCustomNginxBackup returns the raw bytes of one backup (os.ErrNotExist
// when the name is invalid or the file is gone).
func ReadCustomNginxBackup(domain, name string) ([]byte, error) {
	return nginxFile(domain).ReadBackup(name)
}

// RestoreCustomNginx swaps a backup over the live override and reloads nginx.
func RestoreCustomNginx(domain, name string) (cfgedit.RestoreResult, error) {
	return nginxFile(domain).Restore(name, func() error { return NginxReloadFn() })
}

// ValidNginxBackupName reports whether name is a well-formed backup for domain.
func ValidNginxBackupName(domain, name string) bool {
	return nginxFile(domain).ValidBackupName(name)
}
