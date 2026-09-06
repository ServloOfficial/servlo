package nginx

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func headerSite() config.Site {
	return config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
	}
}

func withNginxSettings(site config.Site) VhostData {
	d := vhostDataFor(site)
	d.CertDomain = site.PrimaryDomain()
	d.StaticCacheDays = site.StaticCacheDays
	d.ResponseHeaders = site.ResponseHeaders
	return d
}

func TestNginxSettings_WritesTheSitesOwnResponseHeaders(t *testing.T) {
	site := headerSite()
	site.ResponseHeaders = []config.ResponseHeader{
		{Name: "X-Frame-Options", Value: "SAMEORIGIN"},
		{Name: "Permissions-Policy", Value: "geolocation=()"},
	}

	for _, tmpl := range []string{"vhost.conf.tmpl", "vhost-ssl.conf.tmpl"} {
		out := renderTemplate(t, tmpl, withNginxSettings(site))
		for _, want := range []string{
			`add_header X-Frame-Options "SAMEORIGIN" always;`,
			`add_header Permissions-Policy "geolocation=()" always;`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s is missing %q:\n%s", tmpl, want, out)
			}
		}
	}
}

// always, because a header the browser only gets on a 200 is not a policy. The
// responses that most need a frame or referrer policy are the error pages.
func TestNginxSettings_HeadersApplyToErrorResponsesToo(t *testing.T) {
	site := headerSite()
	site.ResponseHeaders = []config.ResponseHeader{{Name: "X-Frame-Options", Value: "DENY"}}

	out := renderTemplate(t, "vhost.conf.tmpl", withNginxSettings(site))
	if strings.Contains(out, `add_header X-Frame-Options "DENY";`) {
		t.Errorf("the header is not marked always, so an error response drops it:\n%s", out)
	}
}

func TestNginxSettings_StaticCachingExpiresTheAssetsAndNothingElse(t *testing.T) {
	site := headerSite()
	site.StaticCacheDays = 30

	out := renderTemplate(t, "vhost.conf.tmpl", withNginxSettings(site))
	if !strings.Contains(out, "expires 30d;") {
		t.Errorf("no expiry for the cache window:\n%s", out)
	}
	if !strings.Contains(out, `add_header Cache-Control "public, immutable" always;`) {
		t.Errorf("no Cache-Control beside the expiry:\n%s", out)
	}
	// A PHP file cached for thirty days is a site that cannot be deployed, and
	// an HTML page is usually the one file that has to be able to change now.
	block := cacheBlock(t, out)
	for _, unwanted := range []string{"php", "html", "htm"} {
		if strings.Contains(block, unwanted) {
			t.Errorf("the cache location matches %s:\n%s", unwanted, block)
		}
	}
}

// cacheBlock returns the static-cache location, from its `location` line to the
// closing brace at the same indent.
func cacheBlock(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "    location ~*") {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if lines[j] == "    }" {
				return strings.Join(lines[i:j+1], "\n")
			}
		}
	}
	t.Fatalf("no static-cache location in:\n%s", out)
	return ""
}

// nginx's add_header does not merge: a location that sets one discards every
// add_header inherited from the server block. So the static-asset location has
// to repeat what the server declared, or a site's security headers and its HSTS
// silently stop applying to exactly the files a browser fetches the most.
func TestNginxSettings_StaticCachingDoesNotDropTheHeadersAroundIt(t *testing.T) {
	site := headerSite()
	site.Secured = true
	site.StaticCacheDays = 7
	site.ResponseHeaders = []config.ResponseHeader{{Name: "X-Frame-Options", Value: "DENY"}}

	out := renderTemplate(t, "vhost-ssl.conf.tmpl", withNginxSettings(site))

	block := cacheBlock(t, out)
	if !strings.Contains(block, `add_header X-Frame-Options "DENY" always;`) {
		t.Errorf("the cache location drops the site's own header:\n%s", block)
	}
	if !strings.Contains(block, "Strict-Transport-Security") {
		t.Errorf("the cache location drops HSTS:\n%s", block)
	}
}

// Off writes nothing, so nginx's defaults stand and the global config can still
// move them.
func TestNginxSettings_UnsetWritesNothing(t *testing.T) {
	out := renderTemplate(t, "vhost.conf.tmpl", withNginxSettings(headerSite()))

	for _, unwanted := range []string{"expires ", "Cache-Control", "add_header X-"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("wrote %q for a site that set nothing:\n%s", unwanted, out)
		}
	}
}

// A header name and value reach the vhost from the panel and land in a
// directive. A value carrying a quote ends the string it sits in; one carrying
// a brace or a semicolon ends the directive and starts whatever comes next.
func TestNginxSettings_RefusesAHeaderThatWouldBreakOutOfItsDirective(t *testing.T) {
	for _, bad := range []config.ResponseHeader{
		{Name: "X-Bad", Value: `x"; deny all; add_header Y "`},
		{Name: "X-Bad", Value: "x\nadd_header Y z;"},
		{Name: "X-Bad", Value: "x} location / { deny all"},
		{Name: "X-Bad", Value: "x # comment"},
		{Name: "X Bad", Value: "x"},
		{Name: "X-Bad;deny all", Value: "x"},
		{Name: "", Value: "x"},
	} {
		site := headerSite()
		site.ResponseHeaders = []config.ResponseHeader{bad}
		if err := site.ValidateNginxSettings(); err == nil {
			t.Errorf("header %+v was accepted", bad)
		}
	}
}

// Servlo writes HSTS itself from the site's TLS state. A second one from this
// form would emit the header twice, and the shorter max-age is the one a
// browser is entitled to honour, so the form could quietly weaken it.
func TestNginxSettings_RefusesAHeaderServloAlreadyOwns(t *testing.T) {
	site := headerSite()
	site.ResponseHeaders = []config.ResponseHeader{{Name: "strict-transport-security", Value: "max-age=0"}}

	err := site.ValidateNginxSettings()
	if err == nil {
		t.Fatal("a second HSTS header was accepted")
	}
	if !strings.Contains(err.Error(), "Strict-Transport-Security") {
		t.Errorf("error = %q, does not name the header", err)
	}
}

func TestNginxSettings_RefusesADuplicateHeaderAndAnOutOfRangeWindow(t *testing.T) {
	site := headerSite()
	site.ResponseHeaders = []config.ResponseHeader{
		{Name: "X-Frame-Options", Value: "DENY"},
		{Name: "x-frame-options", Value: "SAMEORIGIN"},
	}
	if err := site.ValidateNginxSettings(); err == nil {
		t.Error("the same header twice was accepted, which emits it twice")
	}

	for _, bad := range []int{-1, 4000} {
		site := headerSite()
		site.StaticCacheDays = bad
		if err := site.ValidateNginxSettings(); err == nil {
			t.Errorf("a %d day cache window was accepted", bad)
		}
	}
}
