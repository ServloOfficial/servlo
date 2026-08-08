package nginx

import (
	"fmt"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// The site's own nginx settings: response headers and static-asset caching.
//
// Both render as whole blocks through methods on VhostData rather than
// directives written into each template, for the reason UploadLimit does: six
// templates spelling the same thing six times is six chances for one of them to
// drift, and a site whose header applies on HTTPS but not HTTP is worse than
// one with no header at all.

// staticAssetPattern is what "static assets" means here: the extensions a
// browser fetches repeatedly and a deploy renames or fingerprints. Deliberately
// not .php, and deliberately not .html, which is usually the one file a site
// needs to be able to change without waiting out a cache.
const staticAssetPattern = `\.(?:css|js|mjs|jpg|jpeg|png|gif|webp|avif|svg|ico|woff|woff2|ttf|otf|eot)$`

// ResponseHeaderLines renders the site's own headers as add_header directives.
//
// always, because a header only sent on a 2xx is not a policy: the responses
// that most need a frame or referrer policy are the error pages.
func responseHeaderLines(headers []config.ResponseHeader, indent string) string {
	var b strings.Builder
	for _, h := range headers {
		fmt.Fprintf(&b, "%sadd_header %s %q always;\n", indent, h.Name, h.Value)
	}
	return b.String()
}

// SiteHeaders is the server-level block of the site's own response headers,
// empty when it declares none.
func (d VhostData) SiteHeaders() string {
	return responseHeaderLines(d.ResponseHeaders, "    ")
}

// StaticCache is the location block that lets a browser keep this site's static
// assets, empty when the site sets no window.
//
// It repeats the headers the server block declared, which looks redundant and
// is not: nginx's add_header does not merge down: a location that sets one
// discards every add_header inherited from its parent. Without the repetition,
// turning on caching would silently drop the site's security headers and its
// HSTS for exactly the files a browser fetches the most.
func (d VhostData) StaticCache() string {
	if d.StaticCacheDays <= 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "    location ~* %s {\n", staticAssetPattern)
	fmt.Fprintf(&b, "        expires %dd;\n", d.StaticCacheDays)
	fmt.Fprintf(&b, "        access_log off;\n")
	fmt.Fprintf(&b, "        add_header Cache-Control \"public, immutable\" always;\n")
	// Everything the server block adds, restated because this location's own
	// add_header above would otherwise replace the lot.
	if hsts := d.hstsHeaderLine(); hsts != "" {
		fmt.Fprintf(&b, "%s\n", hsts)
	}
	b.WriteString(responseHeaderLines(d.ResponseHeaders, "        "))
	fmt.Fprintf(&b, "    }\n")
	return b.String()
}

// hstsHeaderLine is the Strict-Transport-Security line the TLS block emits for
// this site, indented for the static-cache location, or empty when the site is
// not secured.
func (d VhostData) hstsHeaderLine() string {
	for _, line := range strings.Split(d.HSTS(), "\n") {
		if strings.Contains(line, "Strict-Transport-Security") {
			return "        " + strings.TrimSpace(line)
		}
	}
	return ""
}

// CanonicalRedirect is the block that sends a site's non-canonical www form to
// its canonical one, empty when the site serves both.
//
// An `if` at server level rather than a second server block. A dedicated block
// would have to be excluded from this one's server_name, carry its own
// certificate, and be duplicated across six templates; `if` with nothing but a
// `return` in it is the one use nginx's own documentation calls safe.
//
// The condition tests the request path as well as the host, and that half is
// load-bearing. A server-level `if` runs in the rewrite phase, before nginx
// picks a location, so without it this would fire ahead of the ACME challenge
// location and 301 a validation for the redirecting host somewhere the
// authority was not asking about. That is exactly the mistake the HTTPS
// redirect used to make.
func (d VhostData) CanonicalRedirect() string {
	// No separate check for the toggle being off: canonicalPair answers false
	// for that as well as for a site the pair does not apply to, and two places
	// deciding it is two places to keep in step.
	from, to, ok := d.canonicalPair()
	if !ok {
		return ""
	}
	return fmt.Sprintf(`    if ($host = %q) {
        set $canonical 1;
    }
    if ($request_uri !~ ^%s) {
        set $canonical "${canonical}1";
    }
    if ($canonical = "11") {
        return 301 $scheme://%s$request_uri;
    }
`, from, acmeChallengePrefix, to)
}

// canonicalPair resolves the two hosts from the domains the vhost was built
// with, so the template data carries the choice and not the arithmetic.
func (d VhostData) canonicalPair() (from, to string, ok bool) {
	site := config.Site{Domains: d.domains(), CanonicalHost: d.CanonicalHost}
	return site.CanonicalRedirect()
}

// domains recovers the site's domain list from the rendered server_name, which
// is where the vhost data already carries it.
func (d VhostData) domains() []string {
	var out []string
	for _, name := range strings.Fields(d.ServerNames) {
		if !strings.HasPrefix(name, "*.") {
			out = append(out, name)
		}
	}
	return out
}
