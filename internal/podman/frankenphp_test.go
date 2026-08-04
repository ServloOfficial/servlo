package podman

import (
	"strings"
	"testing"
)

func TestFrankenPHPContainerName(t *testing.T) {
	got := FrankenPHPContainerName("myapp")
	if got != "servlo-fp-myapp" {
		t.Fatalf("FrankenPHPContainerName: want servlo-fp-myapp, got %s", got)
	}
}

func TestFrankenPHPImage(t *testing.T) {
	// FrankenPHPImage is now the servlo-derived image; the upstream tag it builds
	// FROM is FrankenPHPBaseImage.
	tests := []struct {
		version, wantDerived, wantBase string
	}{
		{"8.2", "localhost/servlo-frankenphp82:local", "docker.io/dunglas/frankenphp:php8.2-alpine"},
		{"8.4", "localhost/servlo-frankenphp84:local", "docker.io/dunglas/frankenphp:php8.4-alpine"},
		{"8.5", "localhost/servlo-frankenphp85:local", "docker.io/dunglas/frankenphp:php8.5-alpine"},
		{"8.1", "localhost/servlo-frankenphp85:local", "docker.io/dunglas/frankenphp:php8.5-alpine"}, // no frankenphp tag → latest
		{"", "localhost/servlo-frankenphp85:local", "docker.io/dunglas/frankenphp:php8.5-alpine"},
	}
	for _, tt := range tests {
		if got := FrankenPHPImage(tt.version); got != tt.wantDerived {
			t.Errorf("FrankenPHPImage(%q): want %s, got %s", tt.version, tt.wantDerived, got)
		}
		if got := FrankenPHPBaseImage(tt.version); got != tt.wantBase {
			t.Errorf("FrankenPHPBaseImage(%q): want %s, got %s", tt.version, tt.wantBase, got)
		}
	}
}

func TestGenerateFrankenPHPQuadlet(t *testing.T) {
	entry := []string{"php", "artisan", "octane:start", "--server=frankenphp"}
	env := map[string]string{"FRANKENPHP_CONFIG": "worker ./public/index.php"}
	content, err := GenerateFrankenPHPQuadlet("myapp", "/home/user/myapp", "8.4", entry, env)
	if err != nil {
		t.Fatalf("GenerateFrankenPHPQuadlet: %v", err)
	}

	mustContain := []string{
		"ContainerName=servlo-fp-myapp",
		"Image=localhost/servlo-frankenphp84:local",
		"Network=servlo",
		"Volume=/home/user/myapp:/home/user/myapp:rw",
		"--workdir=/home/user/myapp",
		`Environment="FRANKENPHP_CONFIG=worker ./public/index.php"`,
		"Exec=php artisan octane:start --server=frankenphp",
		"Restart=always",
		// php.ini parity: the same conf.d inis the FPM container mounts.
		"/usr/local/etc/php/conf.d/98-servlo-user.ini:ro",
		"/usr/local/etc/php/conf.d/95-servlo-shared.ini:ro",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("generated quadlet missing %q\n%s", s, content)
		}
	}
}

func TestShellJoinQuotesWhitespace(t *testing.T) {
	got := shellJoin([]string{"frankenphp", "run", "--with spaces", `has"quote`})
	want := `frankenphp run "--with spaces" "has\"quote"`
	if got != want {
		t.Fatalf("shellJoin:\n  want %s\n  got  %s", want, got)
	}
}

func TestShellJoinEscapesBackslash(t *testing.T) {
	// An arg ending in a backslash must escape it so the closing quote isn't
	// swallowed by the parser on the other side.
	got := shellJoin([]string{"cmd", `path\`})
	want := `cmd "path\\"`
	if got != want {
		t.Fatalf("shellJoin backslash:\n  want %s\n  got  %s", want, got)
	}
}
