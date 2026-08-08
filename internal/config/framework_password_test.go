package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A framework definition writes the service password into a site's .env the
// same way a service preset writes it into a container: through the
// {{password}} placeholder, because the definition is written once and the
// password is generated per install.
//
// Presets have always had that placeholder substituted. Frameworks were parsed
// straight from their bytes, so a definition that used it wrote the six
// characters "{{pass" and the rest of them into DB_PASSWORD, and the site could
// not reach its own database. Every store framework that talks to MySQL or
// Postgres uses it, so this was every Laravel and Symfony site.
func TestParseFramework_SubstitutesTheServicePassword(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	definitions, err := filepath.Glob(filepath.Join("..", "..", "stores", "frameworks", "*", "*.yaml"))
	if err != nil || len(definitions) == 0 {
		t.Fatalf("no framework definitions to read: %v", err)
	}

	used := false
	for _, path := range definitions {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !strings.Contains(string(raw), passwordPlaceholder) {
			continue
		}
		used = true

		fw, err := ParseFramework(raw)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for service, def := range fw.Env.Services {
			for _, kv := range def.Vars {
				if strings.Contains(kv, passwordPlaceholder) {
					t.Errorf("%s: %s wires %q, which is the placeholder rather than the password",
						filepath.Base(filepath.Dir(path)), service, kv)
				}
			}
		}
	}

	// If no definition uses it any more the test above proves nothing, and a
	// silent no-op is how a regression test stops noticing.
	if !used {
		t.Fatal("no framework definition uses the password placeholder, so nothing here was checked")
	}
}

// And the value that lands is the one the services actually run with, not some
// other random string. A password that is merely present but wrong fails the
// same way as the placeholder, one connection attempt later.
func TestParseFramework_WiresThePasswordTheServicesUse(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	password, err := ServicePassword()
	if err != nil {
		t.Fatalf("ServicePassword: %v", err)
	}

	fw, err := ParseFramework([]byte("name: sample\nenv:\n  services:\n    mysql:\n      vars:\n        - DB_PASSWORD={{password}}\n"))
	if err != nil {
		t.Fatalf("ParseFramework: %v", err)
	}
	got := fw.Env.Services["mysql"].Vars
	if len(got) != 1 || got[0] != "DB_PASSWORD="+password {
		t.Errorf("vars = %v, want DB_PASSWORD set to the service password", got)
	}
}
