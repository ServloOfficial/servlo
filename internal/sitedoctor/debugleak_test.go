package sitedoctor

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/envfile"
	"github.com/ServloOfficial/servlo/stores"
)

// Every site servlo runs is on a public domain. A framework's own idea of which
// environment it is in says nothing about that: a checkout whose .env still
// reads APP_ENV=local, which is what copying .env.example gives you, is serving
// the internet all the same. So a debug check that only fires once the site has
// declared itself production is a check that stays quiet for exactly the
// operator who needed it.
//
// Read from the embedded store rather than from a fixture, because the store is
// what a binary ships and the gating lived in the store data.
func TestShippedDebugChecks_DoNotWaitForASiteToCallItselfProduction(t *testing.T) {
	dir := t.TempDir()
	checked := 0

	err := fs.WalkDir(stores.FS(), string(stores.Frameworks), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		data, _ := stores.Read(stores.Frameworks, strings.TrimPrefix(p, string(stores.Frameworks)+"/"))
		var fw config.Framework
		if err := yaml.Unmarshal(data, &fw); err != nil || fw.Doctor == nil {
			return nil
		}
		for _, spec := range fw.Doctor.Checks {
			if spec.Type != "env_combo" || !mentionsDebug(spec.WarnIf) {
				continue
			}
			checked++
			// The shape an operator actually arrives with: the framework's own
			// example env, untouched, with debug left on.
			writeEnv(t, dir, ".env", "APP_ENV=local\nAPP_DEBUG=true\n")
			got := checkEnvCombo(envfile.Reader(path.Join(dir, ".env"), "dotenv"), spec)
			if got.Status != StatusWarn {
				t.Errorf("%s %s: debug is on and the site is public, and the doctor says %q",
					p, spec.Name, got.Status)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no shipped debug check was exercised, so this proves nothing")
	}
}

// The same rule holds for the built-in adapters, which is a second copy of the
// same definitions and drifts from the store the moment one of them is edited
// alone.
func TestBuiltInDebugChecks_DoNotWaitForASiteToCallItselfProduction(t *testing.T) {
	dir := t.TempDir()
	checked := 0
	for _, name := range []string{"laravel", "symfony"} {
		fw, ok := config.GetFramework(name)
		if !ok || fw.Doctor == nil {
			continue
		}
		for _, spec := range fw.Doctor.Checks {
			if spec.Type != "env_combo" || !mentionsDebug(spec.WarnIf) {
				continue
			}
			checked++
			writeEnv(t, dir, ".env", "APP_ENV=local\nAPP_DEBUG=true\n")
			got := checkEnvCombo(envfile.Reader(path.Join(dir, ".env"), "dotenv"), spec)
			if got.Status != StatusWarn {
				t.Errorf("%s %s: debug is on and the site is public, and the doctor says %q",
					name, spec.Name, got.Status)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no built-in debug check was exercised, so this proves nothing")
	}
}

func mentionsDebug(warnIf map[string]string) bool {
	for k := range warnIf {
		if strings.Contains(strings.ToLower(k), "debug") {
			return true
		}
	}
	return false
}
