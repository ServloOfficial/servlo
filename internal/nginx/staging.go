package nginx

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The two directives that make a staging site staging, and the one location
// that has to be exempt from them.
//
// Both are at server level rather than written into each template's locations,
// for the reason SiteHeaders is: six templates spelling the same thing six
// times is six chances for one of them to drift, and a staging site that is
// password-protected on HTTPS but open on HTTP is worse than one that is open,
// because somebody believes it is closed.

// htpasswdRoot is where the credential files are mounted inside the nginx
// container. The host side is config.NginxHtpasswdDir(); the two are different
// paths on different sides of a bind mount, and a test pins them to the quadlet
// so they cannot drift apart silently.
const htpasswdRoot = "/etc/nginx/htpasswd"

// stagingRobots is the header. noindex keeps it out of results, nofollow stops
// it lending the live site's links to a copy, and noarchive stops a cache
// keeping what was on it. Sent always, because an error page on a staging site
// is exactly as indexable as a working one.
const stagingRobots = `add_header X-Robots-Tag "noindex, nofollow, noarchive" always;`

// EnsureContainerMounts creates the host directories the nginx container binds
// that nothing else on the start path already makes.
//
// Podman refuses a bind mount whose source is missing, with a statfs error and
// exit 125, and systemd restarting the unit cannot conjure a directory. So one
// absent path is not a degraded nginx, it is no nginx, every site down, and an
// error message naming a directory rather than whatever added the mount. The
// htpasswd directory is the one that proved it: staging writes a credential
// file into it when a staging site is created, so on a server that had never
// created one it did not exist and the container would not start at all.
//
// Called from EnsureNginxConfig rather than only at install, so an install
// upgraded into a version with a new mount heals on its next start instead of
// staying down until somebody reinstalls.
func EnsureContainerMounts() error {
	for _, dir := range []string{
		config.NginxHtpasswdDir(),
		filepath.Join(config.CertsDir(), "sites"),
		config.RunDir(),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating %s, which the nginx container mounts: %w", dir, err)
		}
	}
	return nil
}

// HtpasswdPathIn is the credential file for a domain as nginx sees it.
func HtpasswdPathIn(domain string) string { return htpasswdRoot + "/" + domain }

// HtpasswdPath is the same file on the host.
func HtpasswdPath(domain string) string {
	return filepath.Join(config.NginxHtpasswdDir(), domain)
}

// RemoveHtpasswd takes a domain's credential file away.
//
// It lives here rather than in internal/staging because this is where the path
// is spelled, and internal/staging imports internal/siteops, so site removal
// could not reach it there. One spelling of a layout is what keeps a caller
// from growing its own copy and quietly missing when the layout moves.
func RemoveHtpasswd(domain string) {
	os.Remove(HtpasswdPath(domain)) //nolint:errcheck — a site with no password is the common case
}

// StagingGuard is the noindex header and the password prompt, empty for an
// ordinary site.
func (d VhostData) StagingGuard() string {
	if d.Staging == nil {
		return ""
	}
	out := "    " + stagingRobots + "\n"
	if d.Staging.User == "" {
		// No credentials yet. The site is still not indexed, and asking for a
		// password against a file that does not exist would answer 500 to
		// everyone rather than 401, which is a broken site rather than a
		// closed one.
		return out
	}
	// The challenge location turns the prompt back off for itself; see
	// ACMEChallenge. It is one location declared in one place rather than a
	// second copy here, because two locations with the same prefix is a
	// configuration nginx refuses to load at all.
	out += fmt.Sprintf("    auth_basic \"Staging\";\n    auth_basic_user_file %s;\n", HtpasswdPathIn(d.Domain))
	return out
}

// stagingChallengeExemption is the line inside the ACME challenge location that
// lets the authority through, empty for a site with no password on it.
//
// Without it a staging site can never be issued a certificate: the authority
// cannot be asked for a password, gets a 401 where it expected the token, and
// reports an authorization failure that reads like a DNS problem.
func (d VhostData) stagingChallengeExemption() string {
	if d.Staging == nil || d.Staging.User == "" {
		return ""
	}
	return "        auth_basic off;\n"
}

// stagingRobotsLine is the header indented for the static-cache location, or
// empty for an ordinary site.
//
// nginx's add_header does not merge: a location that sets one discards every
// add_header inherited from its parent. So turning on static caching would
// otherwise silently make a staging site's images and stylesheets indexable,
// which is enough for a search engine to find the rest of it.
func (d VhostData) stagingRobotsLine() string {
	if d.Staging == nil {
		return ""
	}
	return "        " + stagingRobots
}
