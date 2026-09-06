package nginx

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func redirectData(site config.Site) VhostData {
	d := vhostDataFor(site)
	d.CertDomain = site.PrimaryDomain()
	d.ServerNames = serverNames(site.Domains)
	d.RedirectTo = site.RedirectTo
	d.RedirectPermanent = site.RedirectPermanent
	d.Redirects = site.Redirects
	return d
}

func redirectSite() config.Site {
	return config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
	}
}

// A whole-domain redirect is a domain that has moved: everything on it goes to
// the new home, path and query included, or the redirect drops every deep link
// anybody ever shared.
func TestRedirects_WholeDomainKeepsThePath(t *testing.T) {
	site := redirectSite()
	site.RedirectTo = "https://newshop.example"
	site.RedirectPermanent = true

	out := renderTemplate(t, "vhost.conf.tmpl", redirectData(site))

	if !strings.Contains(out, "return 301 https://newshop.example$request_uri;") {
		t.Errorf("the whole-domain redirect drops the path or the status:\n%s", out)
	}
}

// Temporary by default is the wrong default for a move, but permanent is the
// wrong one for a maintenance detour, so the operator says which. A 301 is
// cached by browsers and is not something they can take back.
func TestRedirects_TemporaryWhenNotMarkedPermanent(t *testing.T) {
	site := redirectSite()
	site.RedirectTo = "https://newshop.example"

	out := renderTemplate(t, "vhost.conf.tmpl", redirectData(site))

	if !strings.Contains(out, "return 302 https://newshop.example$request_uri;") {
		t.Errorf("a redirect nobody marked permanent was written as one:\n%s", out)
	}
}

// The challenge is not a page a visitor asked for. A domain being redirected
// still has to be able to prove itself to the authority, or it can never renew
// the certificate it is serving the redirect over.
func TestRedirects_WholeDomainDoesNotSwallowTheACMEChallenge(t *testing.T) {
	site := redirectSite()
	site.RedirectTo = "https://newshop.example"

	out := renderTemplate(t, "vhost.conf.tmpl", redirectData(site))

	if !strings.Contains(out, "$request_uri !~ ^"+acmeChallengePrefix) {
		t.Errorf("the redirect does not exempt the challenge path:\n%s", out)
	}
}

// A URL-level redirect is one address moving, not the site. An exact match, so
// /old does not also capture /older or /old/thing.
func TestRedirects_URLLevelMatchesExactly(t *testing.T) {
	site := redirectSite()
	site.Redirects = []config.Redirect{
		{From: "/old-page", To: "/new-page", Permanent: true},
		{From: "/brochure.pdf", To: "https://cdn.example/brochure.pdf"},
	}

	out := renderTemplate(t, "vhost.conf.tmpl", redirectData(site))

	if !strings.Contains(out, "location = /old-page {") {
		t.Errorf("the redirect is not an exact match, so it captures paths under it:\n%s", out)
	}
	if !strings.Contains(out, "return 301 /new-page;") {
		t.Errorf("missing the permanent redirect:\n%s", out)
	}
	if !strings.Contains(out, "return 302 https://cdn.example/brochure.pdf;") {
		t.Errorf("missing the temporary redirect to another host:\n%s", out)
	}
}

func TestRedirects_NoneWritesNothing(t *testing.T) {
	out := renderTemplate(t, "vhost.conf.tmpl", redirectData(redirectSite()))

	if strings.Contains(out, "location = ") || strings.Contains(out, "$request_uri;") {
		t.Errorf("a redirect was written for a site that has none:\n%s", out)
	}
}

// A site redirected to a domain it serves itself is a loop the browser reports
// as too many redirects, and with a 301 it is cached, so the operator cannot
// reach the panel-served site to undo it either.
func TestRedirects_RefusesASiteRedirectedToItself(t *testing.T) {
	for _, target := range []string{
		"https://shop.example",
		"http://shop.example/somewhere",
		"https://SHOP.example",
	} {
		site := redirectSite()
		site.RedirectTo = target
		if err := site.ValidateRedirects(); err == nil {
			t.Errorf("redirecting to %q, which this site serves, was accepted", target)
		}
	}
}

// The target lands in a directive and is followed by a browser.
func TestRedirects_RefusesATargetThatIsNotAnAbsoluteURL(t *testing.T) {
	for _, bad := range []string{
		"newshop.example",
		"/somewhere",
		"javascript:alert(1)",
		"https://newshop.example\"; deny all; #",
		"https://newshop.example\nreturn 301 /;",
		"",
	} {
		site := redirectSite()
		site.RedirectTo = bad
		if bad == "" {
			continue // empty is "no redirect", checked elsewhere
		}
		if err := site.ValidateRedirects(); err == nil {
			t.Errorf("whole-domain target %q was accepted", bad)
		}
	}
}

func TestRedirects_RefusesAURLLevelRuleThatIsNotOne(t *testing.T) {
	for _, bad := range []config.Redirect{
		{From: "old-page", To: "/new"},                 // not a path
		{From: "/old page", To: "/new"},                // whitespace ends the directive
		{From: "/old", To: "new"},                      // neither a path nor a URL
		{From: "/old", To: "javascript:alert(1)"},      // not a scheme a browser should follow here
		{From: "/old;deny all", To: "/new"},            // ends the directive
		{From: "/old", To: "/new\nreturn 301 /other;"}, // starts another one
		{From: "", To: "/new"},
		{From: "/old", To: ""},
	} {
		site := redirectSite()
		site.Redirects = []config.Redirect{bad}
		if err := site.ValidateRedirects(); err == nil {
			t.Errorf("redirect %+v was accepted", bad)
		}
	}
}

// Two rules for the same path is a duplicate location, which nginx refuses to
// load, taking every site on the machine with it.
func TestRedirects_RefusesTheSamePathTwice(t *testing.T) {
	site := redirectSite()
	site.Redirects = []config.Redirect{
		{From: "/old", To: "/a"},
		{From: "/old", To: "/b"},
	}

	if err := site.ValidateRedirects(); err == nil {
		t.Fatal("the same path was accepted twice, which is a duplicate location nginx will not load")
	}
}

// A rule pointing at its own path never terminates.
func TestRedirects_RefusesARuleThatPointsAtItself(t *testing.T) {
	site := redirectSite()
	site.Redirects = []config.Redirect{{From: "/loop", To: "/loop"}}

	if err := site.ValidateRedirects(); err == nil {
		t.Fatal("a redirect to its own path was accepted")
	}
}

// A moved domain is redirected from the plain block as well as the secure one.
// The target is an absolute URL, so sending the visitor there directly beats
// bouncing them through the HTTPS version of a domain that has moved.
func TestRedirects_WholeDomainAppliesBeforeTheHTTPSHop(t *testing.T) {
	site := redirectSite()
	site.Secured = true
	site.RedirectTo = "https://newshop.example"

	out := renderTemplate(t, "vhost-ssl.conf.tmpl", redirectData(site))

	blocks := serverBlocks(t, out)
	for _, b := range blocks {
		if !strings.Contains(b, "return 302 https://newshop.example$request_uri;") {
			t.Errorf("a server block does not send the visitor to the new home:\n%s", b)
		}
	}
}

// A URL-level redirect is not on the plain block, and that is deliberate. Its
// target is usually a path, and a relative redirect issued from port 80
// resolves against http://, so the visitor would be sent to the new path over
// plain HTTP and only then redirected to HTTPS. The plain block's one job on a
// secured site is getting to HTTPS; the redirect happens there.
func TestRedirects_URLLevelBelongsToTheSecureBlockOnly(t *testing.T) {
	site := redirectSite()
	site.Secured = true
	site.Redirects = []config.Redirect{{From: "/old", To: "/new", Permanent: true}}

	out := renderTemplate(t, "vhost-ssl.conf.tmpl", redirectData(site))

	blocks := serverBlocks(t, out)
	if strings.Contains(blocks[0], "location = /old {") {
		t.Errorf("the plain block redirects the path, sending the visitor to it over http first:\n%s", blocks[0])
	}
	secure := blocks[len(blocks)-1]
	if !strings.Contains(secure, "location = /old {") {
		t.Errorf("the secure block does not carry the redirect:\n%s", secure)
	}
}
