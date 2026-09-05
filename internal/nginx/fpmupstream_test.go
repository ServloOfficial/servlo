package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A site gets its own pool's socket once that pool exists, and the shared
// container until then.
//
// Gating on the pool file rather than on a config flag is what makes the switch
// safe to ship before every site has been migrated: a vhost pointed at a socket
// nothing is listening on is a site that answers 502, and there is no version
// of this worth being brave about.
func TestFPMUpstream_PrefersTheSitesOwnPoolWhenItExists(t *testing.T) {
	poolDir := t.TempDir()
	sockDir := "/run/servlo/fpm"

	got := FPMUpstream(poolDir, sockDir, "example-com", "servlo-php84-fpm")
	if got.Socket != "" {
		t.Errorf("Socket = %q before a pool exists, want the container", got.Socket)
	}
	if got.Container != "servlo-php84-fpm" {
		t.Errorf("Container = %q", got.Container)
	}

	if err := os.WriteFile(filepath.Join(poolDir, "example-com.conf"), []byte("[example-com]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got = FPMUpstream(poolDir, sockDir, "example-com", "servlo-php84-fpm")
	if got.Socket != "/run/servlo/fpm/example-com.sock" {
		t.Errorf("Socket = %q, want the site's own socket", got.Socket)
	}
}

// Another site's pool must not make this site use a socket that is not its own.
func TestFPMUpstream_IgnoresAPoolBelongingToADifferentSite(t *testing.T) {
	poolDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(poolDir, "other-site.conf"), []byte("[other-site]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := FPMUpstream(poolDir, "/run/servlo/fpm", "example-com", "servlo-php84-fpm")

	if got.Socket != "" {
		t.Errorf("Socket = %q, want the container: this site has no pool", got.Socket)
	}
}

// A handle that is not one cannot be allowed to name a socket path, and the
// safe answer is the shared container rather than a refusal that would take the
// site off the air.
//
// The pool file has to exist for this to be a real test. Checking a bad handle
// against an empty directory proves nothing, because the existence check
// refuses it anyway and the handle guard is never reached: a mutation deleting
// the guard survived exactly that version of this test. So the parent directory
// gets a file the traversal would find.
func TestFPMUpstream_FallsBackForAHandleThatIsNotOne(t *testing.T) {
	parent := t.TempDir()
	poolDir := filepath.Join(parent, "pools")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// poolDir/../evil.conf, which "../evil" resolves to.
	if err := os.WriteFile(filepath.Join(parent, "evil.conf"), []byte("[evil]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"", "../evil", "a/b", "with space"} {
		got := FPMUpstream(poolDir, "/run/servlo/fpm", bad, "servlo-php84-fpm")
		if got.Socket != "" {
			t.Errorf("handle %q produced socket %q", bad, got.Socket)
		}
	}
}

// The generated vhost is what actually reaches nginx, so the switch has to show
// up there rather than only in the decision.
func TestVhost_PassesToTheSocketWhenTheSiteHasAPool(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	poolDir := config.FPMPoolDir("servlo-php84-fpm")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, "acme.conf"), []byte("[acme]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	up := FPMUpstream(poolDir, config.FPMSocketDir(), "acme", "servlo-php84-fpm")
	if up.Socket == "" {
		t.Fatal("the pool was not picked up")
	}
	// unix: is the prefix nginx needs; a bare path is read as a host.
	if !strings.HasPrefix(up.PassTarget(), "unix:") {
		t.Errorf("PassTarget = %q, not a unix socket", up.PassTarget())
	}
	if !strings.HasSuffix(up.PassTarget(), "acme.sock") {
		t.Errorf("PassTarget = %q, not this site's socket", up.PassTarget())
	}
}

func TestFPMUpstream_ContainerPassTargetKeepsThePort(t *testing.T) {
	up := FPMUpstream(t.TempDir(), "/run/servlo/fpm", "acme", "servlo-php84-fpm")

	if up.PassTarget() != "servlo-php84-fpm:9000" {
		t.Errorf("PassTarget = %q, want the container and its port", up.PassTarget())
	}
}

// renderVhostFor renders the plain vhost template the way GenerateVhost does,
// so these assertions are about the file nginx actually reads.
func renderVhostFor(t *testing.T, data VhostData) (string, error) {
	t.Helper()
	raw, err := templateFS.ReadFile("templates/vhost.conf.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("vhost").Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderVhost(tmpl, data)
	return string(out), err
}

// The safety property, asserted on the generated file rather than on the
// decision: a site with no pool must still be pointed at the shared container,
// because a vhost naming a socket nothing listens on is a site answering 502.
func TestVhostTemplate_KeepsTheContainerForASiteWithNoPool(t *testing.T) {
	out, err := renderVhostFor(t, VhostData{
		Domain: "example.com", ServerNames: "example.com", Path: "/srv/example.com",
		PHPVersion: "8.4", PHPVersionShort: "84", FPMContainer: "servlo-php84-fpm",
		PublicDir: "public", RequestTimeout: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fastcgi_pass $fpm:9000;") {
		t.Errorf("a site with no pool lost its container upstream:\n%s", out)
	}
	if strings.Contains(out, "unix:") {
		t.Errorf("a site with no pool was pointed at a socket:\n%s", out)
	}
}

func TestVhostTemplate_UsesTheSocketWhenThePoolIsThere(t *testing.T) {
	out, err := renderVhostFor(t, VhostData{
		Domain: "example.com", ServerNames: "example.com", Path: "/srv/example.com",
		PHPVersion: "8.4", PHPVersionShort: "84", FPMContainer: "servlo-php84-fpm",
		FPMSocket: "/run/servlo/fpm/example-com.sock",
		PublicDir: "public", RequestTimeout: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fastcgi_pass unix:/run/servlo/fpm/example-com.sock;") {
		t.Errorf("the pool socket did not reach the vhost:\n%s", out)
	}
	// Both would be a config nginx refuses.
	if strings.Contains(out, "$fpm:9000") {
		t.Errorf("the vhost carries both upstreams:\n%s", out)
	}
}
