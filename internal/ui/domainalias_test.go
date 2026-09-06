package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/siteops"
)

// aliasHome gives the test its own registry and stops the panel's domain
// handlers from reaching a container.
func aliasHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	prevReissue, prevHosts, prevReload := reissueCertFn, writeHostsFn, nginxReloadFn
	prevPools := siteops.ReloadFPMPoolsFn
	reissueCertFn = func(config.Site) error { return nil }
	writeHostsFn = func() error { return nil }
	nginxReloadFn = func() error { return nil }
	// Not decoration: without this the pool sync shells out to podman, which on
	// a runner that has podman creates real container storage under the scratch
	// HOME and leaves it root-owned, so the temp directory cannot be cleaned up.
	siteops.ReloadFPMPoolsFn = func(string) error { return nil }
	t.Cleanup(func() {
		reissueCertFn, writeHostsFn, nginxReloadFn = prevReissue, prevHosts, prevReload
		siteops.ReloadFPMPoolsFn = prevPools
	})
}

func aliasSite(t *testing.T, secured bool) *config.Site {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(filepath.Join(path, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, PHPVersion: "8.4", PublicDir: "public", Secured: secured,
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	return &site
}

func domainAction(t *testing.T, site *config.Site, action, query string) SiteActionResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/sites/"+site.PrimaryDomain()+"/"+action+"?"+query, nil)
	w := httptest.NewRecorder()
	handleSiteDomainAction(w, r, site, action)

	var res SiteActionResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decoding %s: %v", action, err)
	}
	return res
}

// A secured site that gains an alias needs a certificate covering it, and the
// reissue is the step most likely to fail: the alias is usually added before
// its DNS points here, which is exactly what the pre-flight refuses.
//
// It used to fail silently. The alias was added, the panel said OK, and the
// site went on serving a certificate that did not name it, so every visitor to
// the new domain met a name-mismatch interstitial with nothing in the panel to
// explain it. §3.3: a site never silently serves a certificate that does not
// match.
func TestDomainAdd_SaysSoWhenTheCertificateCouldNotBeReissued(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, true)
	// Deliberately does not name the domain: the warning has to say which
	// domains the certificate is now missing, and reading that back out of the
	// error would prove nothing.
	reissueCertFn = func(config.Site) error {
		return errors.New("the authority does not resolve it to this server")
	}

	res := domainAction(t, site, "domain:add", "name=shop2.example")

	// The alias is still added: the operator asked for it, and pointing DNS is
	// the next thing they do. Refusing would make the order impossible.
	if !res.OK {
		t.Fatalf("the alias was refused outright: %+v", res)
	}
	saved, err := config.FindSite("shop")
	if err != nil || !saved.HasDomain("shop2.example") {
		t.Fatalf("the alias was not saved: %v %+v", err, saved)
	}
	if res.Warning == "" {
		t.Fatal("the failed reissue was swallowed, so the site serves a certificate that does not cover its new domain with nothing said about it")
	}
	if !strings.Contains(res.Warning, "shop2.example") {
		t.Errorf("warning = %q, does not name the domain the certificate is missing", res.Warning)
	}
	// nginx's own words, or the operator cannot tell a DNS problem from a rate
	// limit and has no idea what to fix.
	if !strings.Contains(res.Warning, "the authority does not resolve it") {
		t.Errorf("warning = %q, does not carry the reason", res.Warning)
	}
}

func TestDomainAdd_IsQuietWhenTheCertificateCovered(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, true)

	res := domainAction(t, site, "domain:add", "name=shop2.example")

	if !res.OK || res.Warning != "" {
		t.Errorf("a clean reissue warned anyway: %+v", res)
	}
}

// An unsecured site has no certificate to be wrong about.
func TestDomainAdd_DoesNotReissueForAnUnsecuredSite(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, false)
	called := false
	reissueCertFn = func(config.Site) error { called = true; return nil }

	res := domainAction(t, site, "domain:add", "name=shop2.example")

	if !res.OK {
		t.Fatalf("%+v", res)
	}
	if called {
		t.Error("a certificate was reissued for a site that has none")
	}
}

// Renaming a domain on a secured site is the same exposure: the certificate
// names the old one until it is reissued.
func TestDomainEdit_SaysSoWhenTheCertificateCouldNotBeReissued(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, true)
	reissueCertFn = func(config.Site) error { return errors.New("rate limit reached") }

	res := domainAction(t, site, "domain:edit", "old=shop.example&new=newshop.example")

	if !res.OK {
		t.Fatalf("the rename was refused outright: %+v", res)
	}
	if res.Warning == "" {
		t.Fatal("the failed reissue was swallowed, so the site serves a certificate for a domain it no longer answers to")
	}
	if !strings.Contains(res.Warning, "rate limit") {
		t.Errorf("warning = %q, does not carry the reason", res.Warning)
	}
}

// Adding an alias appends. The primary is what names the vhost file, the
// certificate and APP_URL, so an add that reordered the list would rename the
// site out from under all three.
func TestDomainAdd_LeavesThePrimaryAlone(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, false)

	if res := domainAction(t, site, "domain:add", "name=www.shop.example"); !res.OK {
		t.Fatalf("%+v", res)
	}

	saved, err := config.FindSite("shop")
	if err != nil {
		t.Fatal(err)
	}
	if saved.PrimaryDomain() != "shop.example" {
		t.Errorf("primary is now %q", saved.PrimaryDomain())
	}
	if len(saved.Domains) != 2 || saved.Domains[1] != "www.shop.example" {
		t.Errorf("domains = %v, want the alias appended", saved.Domains)
	}
}

// One domain, one site. Two vhosts claiming the same server_name is a coin
// flip over which one nginx serves.
func TestDomainAdd_RefusesADomainAnotherSiteHolds(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, false)
	other := config.Site{Name: "other", Domains: []string{"other.example"}, Path: t.TempDir(), PHPVersion: "8.4"}
	if err := config.AddSite(other); err != nil {
		t.Fatal(err)
	}

	res := domainAction(t, site, "domain:add", "name=other.example")

	if res.OK {
		t.Fatal("a domain another site already answers for was accepted")
	}
	if !strings.Contains(res.Error, "other") {
		t.Errorf("error = %q, does not name the site holding it", res.Error)
	}
}

func TestDomainAdd_RefusesADomainThatIsNotOne(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, false)

	for _, bad := range []string{"myapp", "10.0.0.1", "shop example", ""} {
		if res := domainAction(t, site, "domain:add", "name="+url.QueryEscape(bad)); res.OK {
			t.Errorf("domain %q was accepted", bad)
		}
	}
}

// A site with no domains has no vhost and cannot be reached at all, so the
// last one is not removable.
func TestDomainRemove_RefusesTheLastDomain(t *testing.T) {
	aliasHome(t)
	site := aliasSite(t, false)

	res := domainAction(t, site, "domain:remove", "name=shop.example")

	if res.OK {
		t.Fatal("the only domain was removed, leaving a site nothing can reach")
	}
}
