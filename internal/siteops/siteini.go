package siteops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A site that runs its own container has no FPM pool, and the pool is where a
// shared-FPM site's PHP settings live.
//
// That is the whole of why raising the upload limit on a FrankenPHP site moved
// nginx's client_max_body_size and nothing else: the vhost half of the pair is
// written for every runtime, the PHP half was written into a file that runtime
// does not have. CLAUDE.md §3.4 says never to expose half of one of these pairs,
// and half is what an upload that passes nginx and is refused by PHP is.
//
// So the same three settings are rendered into an ini the site's container
// already mounts. It is servlo's file, rewritten from the registry on every
// save, and it sits below the operator's own per-site ini so a hand-written key
// still wins.

// writeManagedSiteIni renders a site's PHP settings into the ini its own
// container mounts, and returns whether the content changed.
//
// Only what the operator actually set, exactly as the pool does: restating a
// default here would put it above the shared production ini and stop that file
// being able to move it.
func writeManagedSiteIni(site config.Site) (changed bool, err error) {
	var b strings.Builder
	b.WriteString("; Servlo per-site PHP settings for " + site.Name + ".\n")
	b.WriteString("; Written from the site's settings in the panel. Servlo rewrites this file\n")
	b.WriteString("; on every save, so edit 98-user.ini beside it instead: it loads after this\n")
	b.WriteString("; one and wins.\n")
	if site.MaxUploadMB > 0 {
		fmt.Fprintf(&b, "upload_max_filesize = %dM\n", site.MaxUploadMB)
		fmt.Fprintf(&b, "post_max_size = %dM\n", site.MaxUploadMB)
	}
	if site.MaxExecutionSeconds > 0 {
		fmt.Fprintf(&b, "max_execution_time = %d\n", site.MaxExecutionSeconds)
	}
	if site.MemoryLimitMB > 0 {
		fmt.Fprintf(&b, "memory_limit = %dM\n", site.MemoryLimitMB)
	}

	path := config.SitePHPManagedIniFile(site.Name)
	// podman creates a directory at a bind-mount source that does not exist, and
	// a directory where conf.d expects a file is a container that will not start.
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return false, rmErr
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil && string(existing) == b.String() {
		return false, nil
	}
	// 0644 rather than 0600: the container reads it as its own user, which is
	// not this one. It holds three numbers and no secret.
	return true, os.WriteFile(path, []byte(b.String()), 0644)
}
