package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// Every credential a preset carries has to come from this install, not from the
// repository. A literal in the YAML is a password published to everyone who can
// read the source, and since all sites here run as the same Linux user and reach
// the same database, one such literal is every site's data.
func TestNoPresetShipsALiteralCredential(t *testing.T) {
	// Read from a clean data dir so a store preset another test wrote cannot
	// join the set under audit.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Values a preset once carried verbatim, plus the shapes a new one is
	// likely to reach for. A credential field is allowed exactly one value:
	// the {{password}} placeholder.
	literals := []string{"servlopassword", "password123", "changeme"}
	// Suffixes that name a secret. Deliberately not _ACCESS_KEY: an S3 access
	// key id is an identifier, and the secret half is caught by _SECRET.
	credentialKeys := []string{"_PASSWORD", "_PASS", "_SECRET", "_API_KEY", "_MASTER_KEY"}
	// Values that are settings rather than secrets, e.g. SE_VNC_NO_PASSWORD=1.
	flags := map[string]bool{"0": true, "1": true, "true": true, "false": true, "True": true, "False": true}

	for _, name := range presetNames() {
		data, ok := readPresetBytes(name)
		if !ok {
			t.Fatalf("preset %s could not be read", name)
		}
		body := string(data)
		for _, lit := range literals {
			if strings.Contains(body, lit) {
				t.Errorf("preset %s ships the literal credential %q", name, lit)
			}
		}
		for i, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			key, value, ok := splitCredentialAssignment(trimmed)
			if !ok {
				continue
			}
			named := false
			for _, k := range credentialKeys {
				if strings.HasSuffix(strings.ToUpper(key), k) {
					named = true
					break
				}
			}
			if !named || value == "" || value == "null" || flags[value] {
				continue
			}
			if value != "{{password}}" {
				t.Errorf("%s:%d sets %s to %q, want {{password}}", name, i+1, key, value)
			}
		}
	}
}

// splitCredentialAssignment reads either YAML mapping form (KEY: value) or the
// env-var list form (- KEY=value) a preset uses, whichever the line is.
func splitCredentialAssignment(line string) (key, value string, ok bool) {
	if rest, cut := strings.CutPrefix(line, "- "); cut {
		key, value, ok = strings.Cut(rest, "=")
		return strings.TrimSpace(key), strings.TrimSpace(value), ok
	}
	key, value, ok = strings.Cut(line, ":")
	if !ok || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`), true
}

// The substitution happens where the YAML is read, not field by field where it
// is used, so a placeholder reaches every corner of a definition: container
// environment, the env vars written into a site's .env, connection URLs, the
// command line a service is launched with, and mounted config files.
func TestPresetPasswordIsSubstitutedEverywhere(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	restore := extraPresetsFS
	t.Cleanup(func() { SetExtraPresetsForTest(restore) })
	SetExtraPresetsForTest(fstest.MapFS{
		"secretful.yaml": &fstest.MapFile{Data: []byte(`name: secretful
image: example/secretful:1
environment:
  SECRETFUL_PASSWORD: "{{password}}"
exec: --api-key={{password}}
env_vars:
  - SECRETFUL_SECRET={{password}}
connection_url: secretful://root:{{password}}@127.0.0.1:9999
dynamic_env:
  SECRETFUL_PASSWORDS: repeat_family:secretful={{password}}
files:
  - target: /etc/secretful.conf
    content: |
      password = {{password}}
`)},
	})
	presetCache.Delete("secretful")
	t.Cleanup(func() { presetCache.Delete("secretful") })

	want, err := ServicePassword()
	if err != nil {
		t.Fatalf("ServicePassword: %v", err)
	}

	p, err := LoadPreset("secretful")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := p.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	checks := map[string]string{
		"environment":    svc.Environment["SECRETFUL_PASSWORD"],
		"exec":           svc.Exec,
		"env_vars":       strings.Join(svc.EnvVars, " "),
		"connection_url": svc.ConnectionURL,
		"dynamic_env":    svc.DynamicEnv["SECRETFUL_PASSWORDS"],
	}
	files := PresetFiles("secretful")
	if len(files) != 1 {
		t.Fatalf("PresetFiles returned %d mounts, want 1", len(files))
	}
	checks["files"] = files[0].Content

	for where, got := range checks {
		if strings.Contains(got, "{{password}}") {
			t.Errorf("%s still carries the placeholder: %q", where, got)
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s does not carry this install's password: %q", where, got)
		}
	}
}

// A preset loaded twice has to produce the same password, or a service's
// container and the site .env pointed at it would disagree.
func TestPresetPasswordIsStableAcrossLoads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	presetCache.Delete("mysql")
	t.Cleanup(func() { presetCache.Delete("mysql") })
	first, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	presetCache.Delete("mysql")
	second, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	if first.Environment["MYSQL_ROOT_PASSWORD"] != second.Environment["MYSQL_ROOT_PASSWORD"] {
		t.Fatal("two loads of the same preset produced different passwords")
	}
	if first.Environment["MYSQL_ROOT_PASSWORD"] == "" {
		t.Fatal("mysql has no root password")
	}
}

// A preset fetched from the store goes through the same substitution as a
// built-in one. It is the path a definition published after this build arrives
// by, so a literal there would bypass the check above entirely.
func TestStorePresetPasswordIsSubstituted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	if err := os.MkdirAll(StorePresetsDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := "name: fromstore\nimage: example/fromstore:1\nenvironment:\n  FROMSTORE_PASSWORD: \"{{password}}\"\n"
	if err := os.WriteFile(filepath.Join(StorePresetsDir(), "fromstore.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	presetCache.Delete("fromstore")
	t.Cleanup(func() {
		presetCache.Delete("fromstore")
		_ = os.Remove(filepath.Join(StorePresetsDir(), "fromstore.yaml"))
	})

	want, err := ServicePassword()
	if err != nil {
		t.Fatalf("ServicePassword: %v", err)
	}
	p, err := LoadPreset("fromstore")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	if got := p.Environment["FROMSTORE_PASSWORD"]; got != want {
		t.Fatalf("store preset password = %q, want this install's password", got)
	}
}
