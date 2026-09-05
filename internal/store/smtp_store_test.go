package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"gopkg.in/yaml.v3"
)

// The placeholders sitetpl fills. A definition that asks for one servlo does not
// know does not fail: the literal {{smtp_pass}} is written into the site's live
// env file and the site tries to authenticate with it. That is the whole reason
// this list is checked rather than trusted.
var knownSMTPPlaceholders = map[string]bool{
	"{{smtp_host}}": true, "{{smtp_port}}": true,
	"{{smtp_user}}": true, "{{smtp_password}}": true,
	"{{smtp_user_urlencoded}}": true, "{{smtp_password_urlencoded}}": true,
	"{{smtp_encryption}}": true, "{{smtp_crypto}}": true, "{{smtp_tls}}": true,
	"{{smtp_from_address}}": true, "{{smtp_from_name}}": true,
}

var placeholder = regexp.MustCompile(`\{\{[a-z_]+\}\}`)

func frameworkDefinitions(t *testing.T) map[string]config.Framework {
	t.Helper()
	out := map[string]config.Framework{}
	root := filepath.Join(storesDir, "frameworks")
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the framework store: %v", err)
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, dir.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			rel := filepath.Join(dir.Name(), f.Name())
			body, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			var fw config.Framework
			if err := yaml.Unmarshal(body, &fw); err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			out[rel] = fw
		}
	}
	return out
}

func TestFrameworkStore_SMTPVarsUseKnownPlaceholders(t *testing.T) {
	var checked int
	for rel, fw := range frameworkDefinitions(t) {
		if fw.Env.SMTP == nil {
			continue
		}
		if len(fw.Env.SMTP.Vars) == 0 {
			t.Errorf("%s: declares an smtp block with no vars, which wires nothing", rel)
			continue
		}
		for _, kv := range fw.Env.SMTP.Vars {
			key, value, found := strings.Cut(kv, "=")
			if !found || strings.TrimSpace(key) == "" {
				t.Errorf("%s: %q is not a KEY=VALUE pair", rel, kv)
				continue
			}
			for _, p := range placeholder.FindAllString(value, -1) {
				if !knownSMTPPlaceholders[p] {
					t.Errorf("%s: %s asks for %s, which servlo does not fill", rel, key, p)
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no framework declares mail keys, so this proves nothing")
	}
}

// A framework with an env file servlo manages and no smtp block is a site whose
// Mail card can only say "wire it by hand". Magento is the one that means it:
// its mail settings live in the database, not in env.php. Anything else on this
// list is an oversight.
func TestFrameworkStore_EveryEnvManagedFrameworkDeclaresItsMailKeys(t *testing.T) {
	deliberate := map[string]bool{"magento": true}

	var missing []string
	for rel, fw := range frameworkDefinitions(t) {
		if !fw.HasEnvConfig() || fw.Env.SMTP != nil || deliberate[fw.Name] {
			continue
		}
		missing = append(missing, rel)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these definitions manage an env file but say nothing about mail: %s",
			strings.Join(missing, ", "))
	}
}
