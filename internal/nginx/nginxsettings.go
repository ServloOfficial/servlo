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
