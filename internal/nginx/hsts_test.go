package nginx

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A secured site redirects permanently. 302 was upstream's choice for a local
// development domain that flipped between http and https all day; on a real
// domain with a real certificate the redirect is the truth and a 301 lets
// browsers and intermediaries stop asking.
func TestSecuredVhost_RedirectsPermanently(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4", Secured: true}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	got := vhostBody(t, tmp, "example.com-ssl.conf")
	if strings.Contains(got, "return 302") {
		t.Errorf("the secured vhost still redirects with a 302:\n%s", got)
	}
	if !strings.Contains(got, "return 301 https://") {
		t.Errorf("the secured vhost has no permanent redirect:\n%s", got)
	}
}

// HSTS is what stops the first plain-http request of a session from being
// interceptable at all. `always` matters: without it nginx omits the header on
// error responses, which are exactly the ones an attacker can provoke.
func TestSecuredVhost_SetsHSTS(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4", Secured: true}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	got := vhostBody(t, tmp, "example.com-ssl.conf")
	if !strings.Contains(got, "Strict-Transport-Security") {
		t.Errorf("the secured vhost sets no HSTS header:\n%s", got)
	}
	if !strings.Contains(got, "always;") {
		t.Errorf("the HSTS header is not sent on error responses:\n%s", got)
	}
}

// includeSubDomains would extend the policy to every subdomain, including a
// group secondary a site owner deliberately left on plain http, breaking it in
// every browser that had ever seen the parent. preload is worse: it is
// effectively irreversible. Neither belongs in a default.
func TestSecuredVhost_HSTSDoesNotClaimSubdomainsOrPreload(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4", Secured: true}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	got := vhostBody(t, tmp, "example.com-ssl.conf")
	for _, forbidden := range []string{"includeSubDomains", "preload"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("HSTS carries %s, which a site owner did not ask for:\n%s", forbidden, got)
		}
	}
}

// Every secured site mode gets the same treatment; a proxied site is no less
// worth protecting than a PHP one.
func TestEverySecuredModeRedirectsAndSetsHSTS(t *testing.T) {
	cases := []struct {
		name string
		gen  func(config.Site) error
	}{
		{"custom container", GenerateCustomSSLVhost},
		{"host proxy", GenerateHostProxySSLVhost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", tmp)
			t.Setenv("XDG_DATA_HOME", tmp)

			site := config.Site{
				Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(),
				ContainerPort: 3000, HostPort: 3000, Secured: true,
			}
			if err := tc.gen(site); err != nil {
				t.Fatalf("generating the vhost: %v", err)
			}
			got := vhostBody(t, tmp, "example.com-ssl.conf")
			if !strings.Contains(got, "return 301 https://") {
				t.Errorf("%s does not redirect permanently:\n%s", tc.name, got)
			}
			if !strings.Contains(got, "Strict-Transport-Security") {
				t.Errorf("%s sets no HSTS header:\n%s", tc.name, got)
			}
		})
	}
}

// A paused site still holds a certificate, and that certificate still expires.
// The landing vhost's port 80 block is a bare redirect, so without the
// challenge location a site paused past its 30-day window fails every renewal:
// the authority follows the redirect to 443 and gets the paused page rather
// than the token. This generator is built by hand rather than from a template,
// which is exactly why it needs its own test.
func TestLandingVhost_SecuredSiteStillServesTheChallenge(t *testing.T) {
	site := config.Site{Name: "shop", Domains: []string{"example.com"}, Secured: true}
	got := landingVhostConf(site, "/srv/paused", "example.com.html")

	loc := strings.Index(got, challengeLocation)
	redirect := strings.Index(got, "return 301 https://")
	if loc < 0 {
		t.Fatalf("a paused secured site cannot answer a renewal challenge:\n%s", got)
	}
	if redirect >= 0 && loc > redirect {
		t.Errorf("the challenge location comes after the redirect, so the renewal is bounced to 443:\n%s", got)
	}
}

// An unsecured paused site is where a first certificate gets requested, so it
// needs the location too.
func TestLandingVhost_UnsecuredSiteServesTheChallenge(t *testing.T) {
	site := config.Site{Name: "shop", Domains: []string{"example.com"}}
	if got := landingVhostConf(site, "/srv/paused", "example.com.html"); !strings.Contains(got, challengeLocation) {
		t.Errorf("a paused site does not serve the ACME challenge:\n%s", got)
	}
}
