package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func panelVhostPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(config.NginxConfD(), "servlo-panel.conf")
}

func readPanelVhost(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(panelVhostPath(t))
	if err != nil {
		t.Fatalf("reading the panel vhost: %v", err)
	}
	return string(data)
}

// A panel on a real domain is served by nginx like any site, so it gets a
// certificate through the same flow. Anything else would mean a second
// issuance path to keep working.
func TestEnsurePanelVhost_ServesTheChallengeBeforeAnyRedirect(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := EnsurePanelVhost("panel.example.com", false); err != nil {
		t.Fatalf("EnsurePanelVhost: %v", err)
	}
	conf := readPanelVhost(t)

	challenge := strings.Index(conf, "/.well-known/acme-challenge/")
	if challenge < 0 {
		t.Fatal("the panel vhost does not answer the ACME challenge, so it can never get a certificate")
	}
	// Ahead of any redirect: a challenge bounced to 443 is a renewal that
	// stops working the day the certificate needs it most.
	if redirect := strings.Index(conf, "return 30"); redirect >= 0 && redirect < challenge {
		t.Error("the redirect sits ahead of the ACME challenge location")
	}
}

// Until there is a certificate the vhost has to serve on port 80 alone.
// Emitting a 443 server block that names a certificate file which does not
// exist stops nginx from starting at all, which takes every site with it.
func TestEnsurePanelVhost_NoTLSBlockBeforeThereIsACertificate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := EnsurePanelVhost("panel.example.com", false); err != nil {
		t.Fatalf("EnsurePanelVhost: %v", err)
	}
	conf := readPanelVhost(t)

	if strings.Contains(conf, "ssl_certificate") {
		t.Error("the vhost names a certificate before one has been issued")
	}
	if strings.Contains(conf, "listen 443") {
		t.Error("the vhost listens on 443 before there is a certificate to serve")
	}
}

// Once secured it redirects to HTTPS and carries the same TLS defaults every
// other secured vhost gets, rather than a weaker set written by hand here.
func TestEnsurePanelVhost_SecuredCarriesTheSharedTLSDefaults(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := EnsurePanelVhost("panel.example.com", true); err != nil {
		t.Fatalf("EnsurePanelVhost: %v", err)
	}
	conf := readPanelVhost(t)

	for _, want := range []string{"listen 443 ssl", "ssl_certificate", "ssl_protocols TLSv1.2 TLSv1.3", "return 301"} {
		if !strings.Contains(conf, want) {
			t.Errorf("secured panel vhost missing %q:\n%s", want, conf)
		}
	}
	if !strings.Contains(conf, "Strict-Transport-Security") {
		t.Error("secured panel vhost carries no HSTS header")
	}
}

// The panel is the one host on the box that must never be framed by another
// origin: a clickjacked panel is a clickjacked delete button.
func TestEnsurePanelVhost_RefusesToBeFramed(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := EnsurePanelVhost("panel.example.com", true); err != nil {
		t.Fatalf("EnsurePanelVhost: %v", err)
	}
	conf := readPanelVhost(t)
	if !strings.Contains(conf, "frame-ancestors") && !strings.Contains(conf, "X-Frame-Options") {
		t.Error("the panel vhost lets another origin frame it")
	}
}

// Detaching removes the vhost rather than leaving one that proxies a domain
// the operator has taken back.
func TestRemovePanelVhost(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := EnsurePanelVhost("panel.example.com", false); err != nil {
		t.Fatalf("EnsurePanelVhost: %v", err)
	}
	if err := RemovePanelVhost(); err != nil {
		t.Fatalf("RemovePanelVhost: %v", err)
	}
	if _, err := os.Stat(panelVhostPath(t)); !os.IsNotExist(err) {
		t.Errorf("the panel vhost survived removal: %v", err)
	}
	// Removing one that is not there is how every caller reaches this, so it
	// cannot be an error.
	if err := RemovePanelVhost(); err != nil {
		t.Errorf("removing an absent panel vhost: %v", err)
	}
}
