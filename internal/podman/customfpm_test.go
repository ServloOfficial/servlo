package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func TestCustomFPMBaseVersion(t *testing.T) {
	cases := []struct {
		name string
		from string
		want string
	}{
		{"short-form servlo fpm base", "FROM servlo-php84-fpm:local\n", "8.4"},
		{"older version", "FROM servlo-php82-fpm:local\nWORKDIR /app\n", "8.2"},
		{"no tag", "FROM servlo-php83-fpm\n", "8.3"},
		{"non-servlo base returns empty", "FROM php:8.4-fpm-alpine\n", ""},
		{"unknown version returns empty", "FROM servlo-php99-fpm:local\n", ""},
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Containerfile.servlo"), []byte(c.from), 0644); err != nil {
			t.Fatal(err)
		}
		if got := CustomFPMBaseVersion(dir, nil); got != c.want {
			t.Errorf("%s: CustomFPMBaseVersion = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCustomFPMContainerName(t *testing.T) {
	if got := CustomFPMContainerName("myapp"); got != "servlo-cfpm-myapp" {
		t.Fatalf("CustomFPMContainerName: want servlo-cfpm-myapp, got %s", got)
	}
}

func TestFPMContainerName(t *testing.T) {
	// Custom-FPM site resolves to its per-site container regardless of version.
	cfpm := config.Site{Name: "myapp", Runtime: "fpm-custom"}
	if got := FPMContainerName(cfpm, "8.4"); got != "servlo-cfpm-myapp" {
		t.Errorf("FPMContainerName(custom-fpm): want servlo-cfpm-myapp, got %s", got)
	}
	// Plain PHP site resolves to the shared per-version container.
	plain := config.Site{Name: "myapp"}
	if got := FPMContainerName(plain, "8.4"); got != "servlo-php84-fpm" {
		t.Errorf("FPMContainerName(plain): want servlo-php84-fpm, got %s", got)
	}
	// A custom-container (port-based proxy) site is not custom-FPM; it still
	// resolves to the shared FPM name here (it isn't served by fastcgi).
	proxy := config.Site{Name: "node", ContainerPort: 3000}
	if got := FPMContainerName(proxy, "8.3"); got != "servlo-php83-fpm" {
		t.Errorf("FPMContainerName(proxy): want servlo-php83-fpm, got %s", got)
	}
}

func TestGenerateCustomFPMQuadlet_OverridesImageAndName(t *testing.T) {
	content, err := generateCustomFPMQuadlet("myapp", "8.4")
	if err != nil {
		t.Fatalf("generateCustomFPMQuadlet: %v", err)
	}
	// Image and ContainerName must point at the per-site custom build, not the
	// shared servlo-php84-fpm.
	mustContain := []string{
		"Image=" + CustomImageName("myapp"),
		"ContainerName=servlo-cfpm-myapp",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("quadlet missing %q\n%s", s, content)
		}
	}
	// The shared image/name must be gone (the override replaced them).
	for _, s := range []string{"Image=localhost/servlo-php84-fpm:local", "ContainerName=servlo-php84-fpm"} {
		if strings.Contains(content, s) {
			t.Errorf("quadlet still has shared %q after override", s)
		}
	}
	// It reuses the shared FPM template, so it inherits the servlo mounts; the
	// shared php.ini mount target is a stable proof of that.
	if !strings.Contains(content, "/usr/local/etc/php/conf.d/95-servlo-shared.ini") {
		t.Errorf("custom FPM quadlet did not inherit shared FPM mounts:\n%s", content)
	}
}
