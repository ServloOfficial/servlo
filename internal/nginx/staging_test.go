package nginx

import (
	"strings"
	"testing"
	"text/template"

	"github.com/ServloOfficial/servlo/internal/config"
)

func stagingData(t *testing.T, s *config.SiteStaging) VhostData {
	t.Helper()
	return VhostData{
		Domain:      "staging.acme.example",
		ServerNames: "staging.acme.example",
		Path:        "/srv/staging",
		PublicDir:   "public",
		Staging:     s,
	}
}

// A staging site that search engines index is duplicate content against the
// real site, found late and by somebody else.
func TestStagingGuard_IsNotIndexedAndIsBehindAPassword(t *testing.T) {
	out := stagingData(t, &config.SiteStaging{User: "staging", Hash: "$2a$10$x"}).StagingGuard()

	for _, want := range []string{"X-Robots-Tag", "noindex", "always", "auth_basic ", "auth_basic_user_file"} {
		if !strings.Contains(out, want) {
			t.Errorf("the guard is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "/etc/nginx/htpasswd/staging.acme.example") {
		t.Errorf("the credential file is not where nginx reads it:\n%s", out)
	}
}

// An ordinary site gets neither, or every site on the server would be behind a
// password prompt.
func TestStagingGuard_IsNothingForAnOrdinarySite(t *testing.T) {
	if out := (VhostData{Domain: "acme.example"}).StagingGuard(); out != "" {
		t.Errorf("an ordinary site got a staging guard:\n%s", out)
	}
}

// Credentials that have not been written yet must not produce an auth_basic
// pointing at a file that is not there: nginx answers 500 to everybody for
// that, which is a broken site rather than a closed one.
func TestStagingGuard_AsksForNoPasswordUntilThereIsOne(t *testing.T) {
	out := stagingData(t, &config.SiteStaging{}).StagingGuard()

	if strings.Contains(out, "auth_basic") {
		t.Errorf("a password was demanded before one existed:\n%s", out)
	}
	if !strings.Contains(out, "noindex") {
		t.Errorf("a staging site with no password is still not indexed:\n%s", out)
	}
}

// The authority cannot be asked for a password. Without the exemption a staging
// site can never be issued a certificate, and the failure reads like DNS.
func TestACMEChallenge_LetsTheAuthorityThroughOnAStagingSite(t *testing.T) {
	guarded := stagingData(t, &config.SiteStaging{User: "staging", Hash: "$2a$10$x"}).ACMEChallenge()
	if !strings.Contains(guarded, "auth_basic off;") {
		t.Errorf("the challenge location is behind the password:\n%s", guarded)
	}

	// And an ordinary site's challenge location does not carry a directive
	// that would be meaningless there.
	plain := (VhostData{Domain: "acme.example"}).ACMEChallenge()
	if strings.Contains(plain, "auth_basic") {
		t.Errorf("an ordinary site's challenge location mentions auth_basic:\n%s", plain)
	}
	// One location, not two. Two with the same prefix is a configuration nginx
	// refuses to load at all.
	if n := strings.Count(guarded, "location ^~ "+acmeChallengePrefix); n != 1 {
		t.Errorf("the challenge location appears %d times", n)
	}
}

// nginx's add_header does not merge, so a location that sets one discards every
// header inherited from the server block. Turning static caching on would
// otherwise make a staging site's images and stylesheets indexable, which is
// enough for a search engine to find the rest of it.
func TestStaticCache_KeepsTheNoindexHeader(t *testing.T) {
	d := stagingData(t, &config.SiteStaging{User: "staging", Hash: "$2a$10$x"})
	d.StaticCacheDays = 30

	out := d.StaticCache()
	if !strings.Contains(out, "X-Robots-Tag") {
		t.Errorf("static assets on a staging site are indexable:\n%s", out)
	}
}

// The whole vhost, so the guard is proven to reach the file rather than only
// the method that renders it.
func TestGenerateVhost_CarriesTheGuardIntoTheRenderedFile(t *testing.T) {
	d := stagingData(t, &config.SiteStaging{User: "staging", Hash: "$2a$10$x"})
	d.PHPVersion = "8.4"
	d.FPMContainer = "servlo-php84-fpm"
	d.RequestTimeout = 60

	body, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("vhost").Parse(string(body))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderVhost(tmpl, d)
	if err != nil {
		t.Fatal(err)
	}
	out := string(rendered)
	for _, want := range []string{"X-Robots-Tag", "auth_basic_user_file", "auth_basic off;"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered vhost is missing %q:\n%s", want, out)
		}
	}
}
