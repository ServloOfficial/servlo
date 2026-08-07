package nginx

import (
	"os"
	"regexp"

	"github.com/realrashid/servlo/internal/fpmpool"
)

// Which FPM a site's requests go to.
//
// Servlo is moving from one shared pool per PHP version to one pool per site,
// and the two have to coexist: an install that predates per-site pools has
// sites with no pool of their own, and a vhost pointed at a socket nothing is
// listening on is a site answering 502.
//
// So the switch is gated on the pool file actually being there. A site that has
// one uses it; a site that does not keeps the shared container until it does.
// That makes the change safe to ship before anything has been migrated, which
// is the only way to ship it at all.

// siteHandle mirrors the pool package's: this decides a socket path, so a
// handle that could name a path falls back rather than being trusted.
var siteHandle = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Upstream is where a site's PHP requests go.
type Upstream struct {
	// Socket is the site's own pool socket, empty when it has no pool.
	Socket string
	// Container is the shared per-version FPM container, used when Socket is
	// empty.
	Container string
}

// PassTarget is the value for fastcgi_pass.
func (u Upstream) PassTarget() string {
	if u.Socket != "" {
		// The unix: prefix is not decoration: without it nginx reads the path
		// as a host name.
		return "unix:" + u.Socket
	}
	return u.Container + ":9000"
}

// FPMUpstream decides where a site's PHP requests go.
func FPMUpstream(poolDir, socketDir, site, container string) Upstream {
	up := Upstream{Container: container}
	if !siteHandle.MatchString(site) {
		return up
	}
	if _, err := os.Stat(fpmpool.Path(poolDir, site)); err != nil {
		return up
	}
	up.Socket = fpmpool.SocketPath(socketDir, site)
	return up
}
