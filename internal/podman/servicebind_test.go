package podman

import (
	"strings"
	"testing"
)

// A managed service quadlet, as the presets produce one.
func serviceQuadlet(publish string) string {
	return "[Container]\n" +
		CustomServiceQuadletMarker + "\n" +
		"Image=docker.io/library/mysql:8.4\n" +
		"PublishPort=" + publish + "\n"
}

// The design law, and the one this test exists to hold: a database or cache
// reachable from outside the machine is a database anyone who finds the port
// can attack. Servlo binds them to loopback and nothing else, whatever the LAN
// settings say.
//
// A LAN-exposed MySQL is a development convenience that has no place on a
// server hosting other people's sites, so the exposure applies to nginx only
// and there is no setting that says otherwise.
func TestServiceQuadlet_NeverBindsBeyondLoopback(t *testing.T) {
	for _, lanExposed := range []bool{false, true} {
		out := BindQuadletForLAN("servlo-mysql", serviceQuadlet("127.0.0.1:3306:3306"), lanExposed)
		assertLoopbackOnly(t, out)
	}
}

// A quadlet that arrives already bound to every interface, from an older
// install or an edit by hand, is pulled back to loopback rather than left.
func TestServiceQuadlet_PullsAPublicBindBackToLoopback(t *testing.T) {
	for _, publish := range []string{"3306:3306", "0.0.0.0:3306:3306", "[::]:3306:3306"} {
		out := BindQuadletForLAN("servlo-mysql", serviceQuadlet(publish), true)
		assertLoopbackOnly(t, out)
	}
}

// nginx is the exception, and has to be: it serves the sites, so it binds every
// interface when the operator asks for it.
func TestNginxQuadlet_StillBindsForLAN(t *testing.T) {
	nginx := "[Container]\nImage=docker.io/library/nginx:alpine\nPublishPort=127.0.0.1:80:80\n"
	out := BindQuadletForLAN("servlo-nginx", nginx, true)
	if strings.Contains(out, "PublishPort=127.0.0.1:80:80") {
		t.Errorf("nginx stayed on loopback with LAN exposure on:\n%s", out)
	}
}

func TestNginxQuadlet_LoopbackWhenNotExposed(t *testing.T) {
	nginx := "[Container]\nImage=docker.io/library/nginx:alpine\nPublishPort=80:80\n"
	out := BindQuadletForLAN("servlo-nginx", nginx, false)
	assertLoopbackOnly(t, out)
}

// assertLoopbackOnly fails when any PublishPort line binds somewhere a machine
// on the network could reach.
func assertLoopbackOnly(t *testing.T, quadlet string) {
	t.Helper()
	for _, line := range strings.Split(quadlet, "\n") {
		trimmed := strings.TrimSpace(line)
		value, ok := strings.CutPrefix(trimmed, "PublishPort=")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(value, "127.0.0.1:"), strings.HasPrefix(value, "[::1]:"):
			// Loopback, which is the point.
		default:
			t.Errorf("a service publishes on %q, which is reachable from off the machine", value)
		}
	}
}
