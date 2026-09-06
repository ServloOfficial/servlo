package nginx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ServloOfficial/servlo/internal/cfgedit"
	"github.com/ServloOfficial/servlo/internal/config"
)

// Committing a generated vhost.
//
// A vhost servlo generated is nginx config like any other, and nginx loads its
// whole config or none of it: one file it refuses takes down every site on the
// machine, not only the site whose file it is. CLAUDE.md §3.4 admits no
// exception for config servlo wrote itself, and now that the panel writes an
// operator's own numbers and headers into these files, the difference between
// generated and hand-written config is mostly historical anyway.
//
// So every generated vhost goes through here: back up what is there, write,
// ask nginx, and put the old file back if nginx objects to this one.

// vhostTestFn and vhostReloadFn are `nginx -t` and the reload, indirected so
// tests need no container.
var (
	vhostTestFn   = Test
	vhostReloadFn = Reload
)

// vhostDomain is the domain half of a backup path, so a name that could climb
// out of the backup directory is refused rather than joined into one.
var vhostDomain = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,252}[a-zA-Z0-9])?$`)

// vhostFile is the cfgedit view of one generated vhost.
func vhostFile(confPath string) cfgedit.File {
	name := strings.TrimSuffix(filepath.Base(confPath), ".conf")
	return cfgedit.File{
		Path:    confPath,
		BkpDir:  config.NginxConfDBkp(),
		BkpName: name + ".conf",
	}
}

// commitVhost replaces a generated vhost with rendered, validating first.
//
// An unchanged file is left alone entirely. Every link, every install and every
// quadlet rewrite regenerates every vhost, so backing up and revalidating
// identical bytes would fill the disk with copies of one file and spend a
// container round trip per site to learn nothing.
//
// A validation failure that does not name this file is somebody else's broken
// config, or nginx not being reachable at all, which is the ordinary state
// during install and the first link. Rolling back then would lose a good change
// without fixing anything, and refusing to write would mean a site never gets a
// vhost until nginx is up, which it cannot be until the vhosts exist.
func commitVhost(confPath string, rendered []byte) error {
	if current, err := os.ReadFile(confPath); err == nil && bytes.Equal(current, rendered) {
		return nil
	}

	res, err := vhostFile(confPath).Save(string(rendered), cfgedit.SaveOpts{
		Backup:   true,
		Validate: func(string) (string, error) { return vhostTestFn() },
		Owns:     cfgedit.MentionsFile,
	})
	if err != nil {
		return err
	}
	if !res.OK {
		if res.ValidationOutput != "" {
			return fmt.Errorf("%s: %s", res.Error, strings.TrimSpace(res.ValidationOutput))
		}
		return fmt.Errorf("%s", res.Error)
	}
	return nil
}

// ListVhostBackups returns a domain's generated-vhost backups, newest first.
func ListVhostBackups(domain string) ([]cfgedit.Backup, error) {
	if !vhostDomain.MatchString(domain) {
		return nil, fmt.Errorf("%q is not a usable domain", domain)
	}
	return vhostFile(vhostConfPath(domain)).ListBackups()
}

// ReadVhostBackup returns the bytes of one backup.
func ReadVhostBackup(domain, name string) ([]byte, error) {
	if !vhostDomain.MatchString(domain) {
		return nil, fmt.Errorf("%q is not a usable domain", domain)
	}
	return vhostFile(vhostConfPath(domain)).ReadBackup(name)
}

// RestoreVhost puts a backup back over the live vhost, through the same
// validation as any other write, and reloads nginx.
//
// The next regeneration overwrites it, which is the point: a restore is for
// getting a site serving again now, not for holding config against servlo.
func RestoreVhost(domain, name string) error {
	if !vhostDomain.MatchString(domain) {
		return fmt.Errorf("%q is not a usable domain", domain)
	}
	f := vhostFile(vhostConfPath(domain))
	if !f.ValidBackupName(name) {
		return fmt.Errorf("%q is not a backup of this site's vhost", name)
	}
	body, err := f.ReadBackup(name)
	if err != nil {
		return err
	}
	if err := commitVhost(f.Path, body); err != nil {
		return err
	}
	return vhostReloadFn()
}

// vhostConfPath is where a domain's generated vhost lives.
func vhostConfPath(domain string) string {
	return filepath.Join(config.NginxConfD(), domain+".conf")
}
