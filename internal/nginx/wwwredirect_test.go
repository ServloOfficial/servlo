package nginx

import (
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func wwwData(site config.Site) VhostData {
	d := vhostDataFor(site)
	d.CertDomain = site.PrimaryDomain()
	d.CanonicalHost = site.CanonicalHost
	d.ServerNames = serverNamesWithWildcards(site.Domains)
	return d
}

func wwwSite(canonical string, domains ...string) config.Site {
	return config.Site{
		Name: "shop", Domains: domains,
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
		CanonicalHost: canonical,
	}
}

// One canonical host, everything else redirected to it. Two hostnames serving
// the same content is a split in every metric the site is measured by, and for
// a search engine it is two pages competing with each other.
func TestWWWRedirect_SendsWWWToTheApex(t *testing.T) {
	site := wwwSite("apex", "shop.example", "www.shop.example")

	out := renderTemplate(t, "vhost.conf.tmpl", wwwData(site))

	if !strings.Contains(out, `if ($host = "www.shop.example") {`) {
		t.Errorf("nothing redirects the www host:\n%s", out)
	}
	if !strings.Contains(out, "return 301 $scheme://shop.example$request_uri;") {
		t.Errorf("the redirect does not target the apex, or drops the path:\n%s", out)
	}
}

func TestWWWRedirect_SendsTheApexToWWW(t *testing.T) {
	site := wwwSite("www", "shop.example", "www.shop.example")

	out := renderTemplate(t, "vhost.conf.tmpl", wwwData(site))

	if !strings.Contains(out, `if ($host = "shop.example") {`) {
		t.Errorf("nothing redirects the apex:\n%s", out)
	}
	if !strings.Contains(out, "return 301 $scheme://www.shop.example$request_uri;") {
		t.Errorf("the redirect does not target the www host:\n%s", out)
	}
}

// Off is the default and writes nothing, so a site serving both names on
// purpose keeps doing that.
func TestWWWRedirect_OffWritesNothing(t *testing.T) {
	site := wwwSite("", "shop.example", "www.shop.example")

	out := renderTemplate(t, "vhost.conf.tmpl", wwwData(site))

	if strings.Contains(out, "$host = ") {
		t.Errorf("a redirect was written for a site that asked for none:\n%s", out)
	}
}

// The redirect is a permanent one to a host the site may not answer for at all
// unless both names are aliases. Redirecting to a domain nobody has pointed
// here is a site that 301s every visitor into a dead end, cached by their
// browser.
func TestWWWRedirect_RefusesACanonicalTheSiteDoesNotServe(t *testing.T) {
	site := wwwSite("www", "shop.example")

	if err := site.ValidateCanonicalHost(); err == nil {
		t.Fatal("www was chosen as canonical for a site with no www domain")
	}

	site = wwwSite("apex", "www.shop.example")
	if err := site.ValidateCanonicalHost(); err == nil {
		t.Fatal("the apex was chosen as canonical for a site that only serves www")
	}
}

func TestWWWRedirect_AcceptsOnlyTheTwoDirectionsAndOff(t *testing.T) {
	for _, bad := range []string{"WWW", "yes", "apex.example.com", "none"} {
		site := wwwSite(bad, "shop.example", "www.shop.example")
		if err := site.ValidateCanonicalHost(); err == nil {
			t.Errorf("canonical host %q was accepted", bad)
		}
	}
	for _, ok := range []string{"", "www", "apex"} {
		site := wwwSite(ok, "shop.example", "www.shop.example")
		if err := site.ValidateCanonicalHost(); err != nil {
			t.Errorf("canonical host %q was refused: %v", ok, err)
		}
	}
}

// A site whose primary is a subdomain has no www form to speak of, and
// "www.admin.shop.example" is not a host anybody wants. The pair has to be a
// domain and its own www.
func TestWWWRedirect_RefusesWhenTheDomainsAreNotAWWWPair(t *testing.T) {
	site := wwwSite("apex", "shop.example", "other.example")

	if err := site.ValidateCanonicalHost(); err == nil {
		t.Fatal("a canonical host was accepted for two unrelated domains")
	}
}

// The challenge is not a page a visitor asked for, and a permanent redirect on
// it during issuance is a validation the authority may not follow to a name it
// was not asking about.
func TestWWWRedirect_DoesNotSwallowTheACMEChallenge(t *testing.T) {
	site := wwwSite("apex", "shop.example", "www.shop.example")

	out := renderTemplate(t, "vhost.conf.tmpl", wwwData(site))

	iChallenge := strings.Index(out, "location ^~ "+acmeChallengePrefix)
	iRedirect := strings.Index(out, "$host = ")
	if iChallenge < 0 || iRedirect < 0 {
		t.Fatalf("challenge=%d redirect=%d:\n%s", iChallenge, iRedirect, out)
	}
	// An `if` at server level runs in the rewrite phase, before nginx picks a
	// location, so writing it above the challenge is not enough: it has to test
	// the path too. The same mistake the HTTPS redirect made.
	if !strings.Contains(out, "$request_uri !~ ^"+acmeChallengePrefix) {
		t.Errorf("the redirect does not exempt the challenge path, so it fires before the challenge location is chosen:\n%s", out)
	}
}

// Both hosts stay in server_name and in the certificate. The redirecting one
// still has to be answered, over TLS, or a visitor typing https://www gets a
// certificate warning instead of a redirect.
func TestWWWRedirect_KeepsBothHostsServed(t *testing.T) {
	site := wwwSite("apex", "shop.example", "www.shop.example")
	site.Secured = true

	out := renderTemplate(t, "vhost-ssl.conf.tmpl", wwwData(site))

	for _, block := range serverBlocks(t, out) {
		if !strings.Contains(block, "www.shop.example") {
			t.Errorf("a server block does not answer for the redirecting host:\n%s", block)
		}
	}
	if !strings.Contains(out, "$host = ") {
		t.Errorf("the secured vhost has no canonical redirect:\n%s", out)
	}
}
