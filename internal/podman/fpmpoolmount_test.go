package podman

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// homedIn points the XDG paths at a scratch home and returns it. The socket
// test needs the servlo data directory to sit under the home directory the way
// it does on a real install, because that is the mount it travels through.
func homedIn(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

// The pools are only pools if the master process reads them. FPM includes
// /usr/local/etc/php-fpm.d/*.conf, so the directory servlo writes them to has
// to be mounted there.
func TestFPMQuadlet_MountsThePoolDirectoryWhereFPMReadsPools(t *testing.T) {
	homedIn(t)

	content, err := renderFPMQuadletContent("8.4")
	if err != nil {
		t.Fatalf("renderFPMQuadletContent: %v", err)
	}

	want := "Volume=" + config.FPMPoolDir("servlo-php84-fpm") + ":/usr/local/etc/php-fpm.d:ro"
	if !strings.Contains(content, want) {
		t.Errorf("the quadlet does not mount the pool directory:\nwant a line %q\ngot:\n%s", want, content)
	}
}

// The pool's socket has to exist somewhere both this container and nginx can
// see at the same absolute path, or the pool's listen address and the vhost's
// fastcgi_pass name different things.
//
// Today that works through the broad %h:%h home mount. This asserts the
// directory is reachable rather than asserting how, so narrowing that mount
// later fails here rather than silently breaking every pool.
func TestFPMQuadlet_CanReachTheSocketDirectory(t *testing.T) {
	home := homedIn(t)

	content, err := renderFPMQuadletContent("8.4")
	if err != nil {
		t.Fatal(err)
	}

	sockDir := config.FPMSocketDir()
	mounted := false
	for _, line := range strings.Split(content, "\n") {
		spec, ok := strings.CutPrefix(strings.TrimSpace(line), "Volume=")
		if !ok {
			continue
		}
		// systemd expands %h to the home directory, so the comparison has to.
		spec = strings.ReplaceAll(spec, "%h", home)
		host, rest, ok := strings.Cut(spec, ":")
		if !ok {
			continue
		}
		container, _, _ := strings.Cut(rest, ":")
		// Only a mount that puts the host path at the same path inside can make
		// the two sides agree on the socket.
		if host == container && strings.HasPrefix(sockDir, host+"/") {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("no volume makes %q reachable at the same path inside the container:\n%s", sockDir, content)
	}
}

// poolMount returns the host directory a rendered quadlet mounts as php-fpm.d.
func poolMount(t *testing.T, content string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		spec, ok := strings.CutPrefix(strings.TrimSpace(line), "Volume=")
		if !ok {
			continue
		}
		host, rest, ok := strings.Cut(spec, ":")
		if !ok {
			continue
		}
		if container, _, _ := strings.Cut(rest, ":"); container == "/usr/local/etc/php-fpm.d" {
			return host
		}
	}
	t.Fatalf("no php-fpm.d mount in:\n%s", content)
	return ""
}

// Every FPM master reads every pool in the directory mounted at php-fpm.d, and
// a pool's socket path is the same whichever master created it. So two masters
// sharing a directory both define every site's pool and both bind every
// socket, and whichever started last is serving all of them: a site pinned to
// 8.3 quietly runs on 8.4. The directory has to be the container's own.
func TestFPMQuadlet_DoesNotSharePoolsBetweenVersions(t *testing.T) {
	homedIn(t)

	eightThree, err := renderFPMQuadletContent("8.3")
	if err != nil {
		t.Fatal(err)
	}
	eightFour, err := renderFPMQuadletContent("8.4")
	if err != nil {
		t.Fatal(err)
	}

	if a, b := poolMount(t, eightThree), poolMount(t, eightFour); a == b {
		t.Errorf("8.3 and 8.4 both read their pools from %q, so each master defines the other's sites and binds their sockets", a)
	}
}

// A custom-FPM site runs its own container from this same template, so it
// inherits the mount and the same collision with it.
func TestCustomFPMQuadlet_DoesNotSharePoolsWithTheSharedContainer(t *testing.T) {
	homedIn(t)

	shared, err := renderFPMQuadletContent("8.4")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := generateCustomFPMQuadlet("myapp", "8.4")
	if err != nil {
		t.Fatal(err)
	}

	if a, b := poolMount(t, shared), poolMount(t, custom); a == b {
		t.Errorf("the custom-FPM container reads the shared container's pools from %q", a)
	}
}
