package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// The challenge location is what makes HTTP-01 possible at all: Let's Encrypt
// fetches http://<domain>/.well-known/acme-challenge/<token> over port 80 and
// compares the body against what it handed the client. Every vhost servlo
// writes has to answer that path, or the site cannot get a certificate.
const challengeLocation = "location ^~ /.well-known/acme-challenge/"

func vhostBody(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "servlo", "nginx", "conf.d", name))
	if err != nil {
		t.Fatalf("reading the generated vhost: %v", err)
	}
	return string(body)
}

func TestGenerateVhost_ServesTheACMEChallenge(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4"}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}

	got := vhostBody(t, tmp, "example.com.conf")
	if !strings.Contains(got, challengeLocation) {
		t.Errorf("a plain HTTP vhost does not serve the ACME challenge:\n%s", got)
	}
	if !strings.Contains(got, acmeChallengeRoot) {
		t.Errorf("the challenge location does not point at the shared webroot:\n%s", got)
	}
}

// The SSL vhost is the one that can get this wrong. Its port 80 block exists
// only to redirect to HTTPS, and a bare redirect would bounce the renewal
// challenge to a port Let's Encrypt does not use for HTTP-01. The challenge
// has to be answered before the redirect, or every renewal on a secured site
// depends on the redirect chase working, which is not a guarantee to build on.
func TestGenerateSSLVhost_AnswersTheChallengeBeforeRedirecting(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4", Secured: true}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}

	got := vhostBody(t, tmp, "example.com-ssl.conf")
	loc := strings.Index(got, challengeLocation)
	redirect := strings.Index(got, "return 302 https://")
	if loc < 0 {
		t.Fatalf("the SSL vhost does not serve the ACME challenge:\n%s", got)
	}
	if redirect < 0 {
		t.Fatalf("the SSL vhost lost its HTTPS redirect:\n%s", got)
	}
	if loc > redirect {
		t.Errorf("the challenge location comes after the redirect, so a renewal request is bounced to 443:\n%s", got)
	}
}

// A proxied or custom-container site gets its certificate the same way, so it
// needs the same location. Missing it on one site mode is the kind of gap that
// only shows up as a renewal failure months later.
func TestEveryVhostModeServesTheChallenge(t *testing.T) {
	cases := []struct {
		name string
		gen  func(config.Site) error
		file string
	}{
		{"custom container", func(s config.Site) error { return GenerateCustomVhost(s) }, "example.com.conf"},
		{"custom container over TLS", func(s config.Site) error { return GenerateCustomSSLVhost(s) }, "example.com-ssl.conf"},
		{"host proxy", func(s config.Site) error { return GenerateHostProxyVhost(s) }, "example.com.conf"},
		{"host proxy over TLS", func(s config.Site) error { return GenerateHostProxySSLVhost(s) }, "example.com-ssl.conf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", tmp)
			t.Setenv("XDG_DATA_HOME", tmp)

			site := config.Site{
				Name:          "myapp",
				Domains:       []string{"example.com"},
				Path:          t.TempDir(),
				ContainerPort: 3000,
				HostPort:      3000,
			}
			if err := tc.gen(site); err != nil {
				t.Fatalf("generating the vhost: %v", err)
			}
			if got := vhostBody(t, tmp, tc.file); !strings.Contains(got, challengeLocation) {
				t.Errorf("%s does not serve the ACME challenge:\n%s", tc.name, got)
			}
		})
	}
}

// The webroot is bind-mounted into the nginx container, so the path nginx is
// told to serve from and the path servlo writes tokens to have to agree. They
// are different paths on different sides of the mount, which is exactly the
// pair that drifts silently.
func TestChallengeRootMatchesTheQuadletMount(t *testing.T) {
	quadlet, err := os.ReadFile("../podman/quadlets/servlo-nginx.container")
	if err != nil {
		t.Fatalf("reading the nginx quadlet: %v", err)
	}
	if !strings.Contains(string(quadlet), acmeChallengeRoot) {
		t.Errorf("the nginx quadlet does not mount the challenge webroot at %s:\n%s", acmeChallengeRoot, quadlet)
	}
}
