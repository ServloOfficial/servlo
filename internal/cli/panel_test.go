package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
)

func panelTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// The domain goes straight into a generated nginx config, so anything that
// could close a block or start a directive has to be refused before it is
// written rather than after nginx fails to parse it.
func TestValidatePanelDomain_RefusesWhatWouldEscapeTheConfig(t *testing.T) {
	for _, domain := range []string{
		"",
		"panel",
		"*.example.com",
		"panel.example.com;",
		"panel.example.com { root /etc",
		"panel example.com",
		"panel.example.com\nserver_name evil.com",
		"$host.example.com",
		"../../etc/passwd",
	} {
		if err := validatePanelDomain(domain); err == nil {
			t.Errorf("validatePanelDomain(%q) accepted it", domain)
		}
	}
}

func TestValidatePanelDomain_AcceptsAnOrdinaryFQDN(t *testing.T) {
	for _, domain := range []string{"panel.example.com", "servlo.example.co.uk", "a.b.c.example.com"} {
		if err := validatePanelDomain(domain); err != nil {
			t.Errorf("validatePanelDomain(%q) = %v", domain, err)
		}
	}
}

// Attaching a domain writes a vhost that answers the ACME challenge and
// nothing more, because there is no certificate yet. A vhost that named one
// would stop nginx from starting, taking every site with it.
func TestApplyPanelDomain_WritesAnUnsecuredVhostFirst(t *testing.T) {
	panelTestHome(t)
	cfg := &config.GlobalConfig{}
	cfg.UI.Domain = "panel.example.com"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	if err := ApplyPanelDomain(); err != nil {
		t.Fatalf("ApplyPanelDomain: %v", err)
	}
	data, err := os.ReadFile(nginx.PanelVhostPath())
	if err != nil {
		t.Fatalf("reading the panel vhost: %v", err)
	}
	conf := string(data)
	if !strings.Contains(conf, "panel.example.com") {
		t.Error("the vhost does not name the panel domain")
	}
	if strings.Contains(conf, "ssl_certificate") {
		t.Error("the vhost names a certificate that has not been issued")
	}
}

// No domain means no vhost. Leaving one behind would keep nginx answering for
// a name the operator has taken back.
func TestApplyPanelDomain_RemovesTheVhostWhenThereIsNoDomain(t *testing.T) {
	panelTestHome(t)
	cfg := &config.GlobalConfig{}
	cfg.UI.Domain = "panel.example.com"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	if err := ApplyPanelDomain(); err != nil {
		t.Fatalf("ApplyPanelDomain: %v", err)
	}

	cfg.UI.Domain = ""
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	if err := ApplyPanelDomain(); err != nil {
		t.Fatalf("ApplyPanelDomain: %v", err)
	}
	if _, err := os.Stat(nginx.PanelVhostPath()); !os.IsNotExist(err) {
		t.Errorf("the panel vhost survived the domain being removed: %v", err)
	}
}

// writeSecuredSites stages a registry with one secured site and one plain one,
// so the panel's domain has something to be missing from.
func writeSecuredSites(t *testing.T) {
	t.Helper()
	dir := config.DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "sites:\n" +
		"  - name: alpha\n    path: /srv/alpha\n    domains: [alpha.example]\n    secured: true\n" +
		"  - name: beta\n    path: /srv/beta\n    domains: [beta.example]\n"
	if err := os.WriteFile(config.SitesFile(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePanelCert(t *testing.T, domain string) {
	t.Helper()
	certPath, keyPath := certs.SitePaths(domain)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{certPath, keyPath} {
		if err := os.WriteFile(p, []byte("stand-in"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The panel's certificate expires like any other, and the panel is what the
// operator would sign in to in order to be told. A check that reads only the
// registry never sees it.
func TestSecuredCertDomains_IncludesThePanelsOwn(t *testing.T) {
	panelTestHome(t)
	writeSecuredSites(t)
	cfg := &config.GlobalConfig{}
	cfg.UI.Domain = "panel.example.com"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	writePanelCert(t, "panel.example.com")

	got := securedCertDomains()
	if !slices.Contains(got, "panel.example.com") {
		t.Errorf("the panel's own certificate is not checked: %v", got)
	}
	if !slices.Contains(got, "alpha.example") || slices.Contains(got, "beta.example") {
		t.Errorf("the secured sites are wrong: %v", got)
	}
}

// A domain attached but never secured has no certificate, so reporting one as
// missing would be a warning about a step the operator has not taken yet.
func TestSecuredCertDomains_SkipsAPanelDomainWithNoCertificate(t *testing.T) {
	panelTestHome(t)
	writeSecuredSites(t)
	cfg := &config.GlobalConfig{}
	cfg.UI.Domain = "panel.example.com"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	if got := securedCertDomains(); slices.Contains(got, "panel.example.com") {
		t.Errorf("an unsecured panel domain was reported as having a certificate: %v", got)
	}
}
