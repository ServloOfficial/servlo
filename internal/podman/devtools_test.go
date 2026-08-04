package podman

import (
	"os"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func TestDevtoolsIni_SubstitutesPlaceholders(t *testing.T) {
	withTempXDG(t)
	ini, err := DevtoolsIni()
	if err != nil {
		t.Fatalf("DevtoolsIni: %v", err)
	}
	if strings.Contains(ini, "{{") {
		t.Errorf("ini still has unsubstituted placeholders: %s", ini)
	}
	if !strings.Contains(ini, "servlo.devtools_host="+config.DevtoolsBridgeTarget()) {
		t.Errorf("ini missing host target: %s", ini)
	}
	if !strings.Contains(ini, "servlo.devtools_kinds=query") {
		t.Errorf("ini missing kinds: %s", ini)
	}
	// Capture shares the debug bridge's sentinel, so the extension reads the
	// same enabled.flag rather than a separate devtools.flag.
	if !strings.Contains(ini, "servlo.devtools_flag=/usr/local/etc/servlo/enabled.flag") {
		t.Errorf("ini flag should point at the shared enabled.flag: %s", ini)
	}
}

func TestEnsureDevtoolsAssets_WritesIni(t *testing.T) {
	withTempXDG(t)
	if err := EnsureDevtoolsAssets(); err != nil {
		t.Fatalf("EnsureDevtoolsAssets: %v", err)
	}
	b, err := os.ReadFile(config.DevtoolsIniFile())
	if err != nil {
		t.Fatalf("read ini: %v", err)
	}
	if !strings.Contains(string(b), "servlo.devtools_host=") {
		t.Errorf("ini content unexpected: %s", string(b))
	}
}

// The Containerfile compiles servlo_devtools in the builder stage and carries a
// marker hashing the source, so any change to the C drifts the image hash and
// triggers a base rebuild + a NeedsFPMRebuild for updating users. If this fails
// after editing the extension, update the `servlo_devtools-src-sha256:` line in
// servlo-php-fpm.Containerfile to the printed value.
func TestDevtoolsSourceMarkerInSync(t *testing.T) {
	want, err := devtoolsSourceHash()
	if err != nil {
		t.Fatalf("devtoolsSourceHash: %v", err)
	}
	tmpl, err := GetQuadletTemplate("servlo-php-fpm.Containerfile")
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	marker := "servlo_devtools-src-sha256: " + want
	if !strings.Contains(tmpl, marker) {
		t.Errorf("Containerfile marker out of date; expected %q. Update the servlo_devtools-src-sha256 line.", marker)
	}
	// The builder must actually compile and enable the extension.
	if !strings.Contains(tmpl, "COPY internal/podman/devtools") || !strings.Contains(tmpl, "docker-php-ext-enable servlo_devtools") {
		t.Error("Containerfile no longer compiles/enables servlo_devtools in the builder stage")
	}
}

func TestWriteDevtoolsSource_StagesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := writeDevtoolsSource(dir); err != nil {
		t.Fatalf("writeDevtoolsSource: %v", err)
	}
	// Staged at the repo-relative path the Containerfile's COPY expects.
	for _, name := range []string{"config.m4", "php_servlo_devtools.h", "servlo_devtools.c"} {
		if _, err := os.Stat(dir + "/internal/podman/devtools/" + name); err != nil {
			t.Errorf("expected staged %s: %v", name, err)
		}
	}
}
