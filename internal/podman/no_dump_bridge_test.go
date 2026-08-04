package podman

import (
	"strings"
	"testing"
)

// dumpBridgeMarkers are the strings that would put the dump()/dd() bridge back
// into a container: the auto_prepend file's own directory, the two conf.d inis
// that armed the bridge and the devtools extension, and the ini directive
// itself. PRD §4 deletes the bridge, and a mount is what would silently revive
// it, so this asserts on generated unit text rather than on a runtime flag.
var dumpBridgeMarkers = []string{
	"auto_prepend",
	"/usr/local/etc/servlo",
	"97-servlo-dump.ini",
	"96-servlo-devtools.ini",
	"servlo_devtools",
	"dump-bridge.php",
}

func assertNoDumpBridge(t *testing.T, label, content string) {
	t.Helper()
	for _, m := range dumpBridgeMarkers {
		if strings.Contains(content, m) {
			t.Errorf("%s still carries the dump bridge (%q):\n%s", label, m, content)
		}
	}
}

// The shared PHP-FPM unit is the one every site is served from, so it is the
// unit that matters most.
func TestGeneratedFPMQuadlet_HasNoDumpBridgeMount(t *testing.T) {
	withTempXDG(t)
	content, err := renderFPMQuadletContent("8.4")
	if err != nil {
		t.Fatalf("renderFPMQuadletContent: %v", err)
	}
	assertNoDumpBridge(t, "generated FPM quadlet", content)
}

// A FrankenPHP site runs its own container and used to be mounted for parity
// with FPM, so it needs the same proof.
func TestGeneratedFrankenPHPQuadlet_HasNoDumpBridgeMount(t *testing.T) {
	withTempXDG(t)
	content, err := GenerateFrankenPHPQuadlet("myapp", "/home/user/myapp", "8.4", nil, nil)
	if err != nil {
		t.Fatalf("GenerateFrankenPHPQuadlet: %v", err)
	}
	assertNoDumpBridge(t, "generated FrankenPHP quadlet", content)
}

// The per-site custom FPM container reuses the shared template, so a marker
// reintroduced there would reach it too.
func TestGeneratedCustomFPMQuadlet_HasNoDumpBridgeMount(t *testing.T) {
	withTempXDG(t)
	content, err := generateCustomFPMQuadlet("myapp", "8.4")
	if err != nil {
		t.Fatalf("generateCustomFPMQuadlet: %v", err)
	}
	assertNoDumpBridge(t, "generated custom FPM quadlet", content)
}

// The Containerfiles are where the devtools extension was compiled in, so a
// rebuild would carry it back even with every mount removed.
func TestContainerfiles_DoNotBuildTheDevtoolsExtension(t *testing.T) {
	for _, name := range []string{"servlo-php-fpm.Containerfile", "servlo-frankenphp.Containerfile"} {
		content, err := GetQuadletTemplate(name)
		if err != nil {
			t.Fatalf("GetQuadletTemplate(%s): %v", name, err)
		}
		assertNoDumpBridge(t, name, content)
	}
}
