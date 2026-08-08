package nginx

import (
	"strings"
	"testing"
)

// serverBlocks splits a rendered vhost into its server { } blocks.
func serverBlocks(t *testing.T, out string) []string {
	t.Helper()
	var blocks []string
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "server {" {
			continue
		}
		depth := 0
		for j := i; j < len(lines); j++ {
			depth += strings.Count(lines[j], "{") - strings.Count(lines[j], "}")
			if depth == 0 {
				blocks = append(blocks, strings.Join(lines[i:j+1], "\n"))
				break
			}
		}
	}
	if len(blocks) == 0 {
		t.Fatalf("no server block in:\n%s", out)
	}
	return blocks
}

// serverLevelReturn reports whether a server block has a `return` outside every
// location, which is the part that matters: nginx runs the server rewrite phase
// before it picks a location, so such a return fires for every request and no
// location in the block is ever reached.
func serverLevelReturn(block string) bool {
	depth := 0
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if depth == 1 && strings.HasPrefix(trimmed, "return ") {
			return true
		}
		depth += strings.Count(l, "{") - strings.Count(l, "}")
	}
	return false
}

func hasACMELocation(block string) bool {
	return strings.Contains(block, "location ^~ "+acmeChallengePrefix)
}

// The whole certificate story rests on this one path staying reachable.
//
// A secured site's port-80 block redirects everything to HTTPS, and that
// redirect used to be a server-level `return`. nginx executes the server
// rewrite phase before it selects a location, so the return fired first and the
// challenge location under it was dead config. The challenge was 301'd to
// HTTPS, where there was no challenge location at all, and the request fell
// through to the site: an HTML page where the authority expected a token.
//
// Nothing about that is visible until a certificate comes up for renewal, sixty
// days after the site was set up and working.
func TestSecuredVhost_DoesNotRedirectTheACMEChallengeIntoNowhere(t *testing.T) {
	for _, tmpl := range []string{"vhost-ssl.conf.tmpl", "vhost-custom-ssl.conf.tmpl", "vhost-hostproxy-ssl.conf.tmpl"} {
		out := renderTemplate(t, tmpl, secureVhostData())
		blocks := serverBlocks(t, out)

		plain := blocks[0]
		if !strings.Contains(plain, "listen 80;") {
			t.Fatalf("%s: the first block is not the plain-HTTP one:\n%s", tmpl, plain)
		}
		if !hasACMELocation(plain) {
			t.Errorf("%s: no challenge location on port 80:\n%s", tmpl, plain)
		}
		if serverLevelReturn(plain) {
			t.Errorf("%s: the redirect is a server-level return, so it runs before the challenge location is even chosen:\n%s", tmpl, plain)
		}
		// The redirect still has to happen for everything else.
		if !strings.Contains(plain, "return 301 https://") {
			t.Errorf("%s: nothing redirects to HTTPS:\n%s", tmpl, plain)
		}
	}
}

// And it is served over HTTPS too. The authority follows the redirect to the
// secure side, and anything else in front of this site might redirect as well,
// so the challenge has to be answerable on both.
func TestSecuredVhost_ServesTheACMEChallengeOverHTTPSAsWell(t *testing.T) {
	for _, tmpl := range []string{"vhost-ssl.conf.tmpl", "vhost-custom-ssl.conf.tmpl", "vhost-hostproxy-ssl.conf.tmpl"} {
		out := renderTemplate(t, tmpl, secureVhostData())
		blocks := serverBlocks(t, out)

		secure := blocks[len(blocks)-1]
		if !strings.Contains(secure, "listen 443") {
			t.Fatalf("%s: the last block is not the TLS one:\n%s", tmpl, secure)
		}
		if !hasACMELocation(secure) {
			t.Errorf("%s: the challenge cannot be answered over HTTPS, so a renewal that follows the redirect gets the site instead of the token:\n%s", tmpl, secure)
		}
	}
}

// The plain vhost serves the site on port 80 and redirects nothing, so the
// challenge is reachable there for the first issuance.
func TestPlainVhost_ServesTheACMEChallenge(t *testing.T) {
	out := renderTemplate(t, "vhost.conf.tmpl", vhostDataFor(headerSite()))

	if !hasACMELocation(out) {
		t.Errorf("no challenge location, so a site can never get its first certificate:\n%s", out)
	}
}

func secureVhostData() VhostData {
	site := headerSite()
	site.Secured = true
	d := vhostDataFor(site)
	d.CertDomain = site.PrimaryDomain()
	d.CustomContainer = "servlo-custom-shop"
	d.CustomPort = 3000
	d.UpstreamHost = "host.containers.internal"
	d.UpstreamPort = 5173
	return d
}
