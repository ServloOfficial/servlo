package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/nginx"
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
