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
// can attack. Servlo binds them to loopback and nothing else.
//
// A publicly bound MySQL is a development convenience that has no place on a
// server hosting other people's sites, so the public form applies to nginx
// only and there is no setting that says otherwise.
func TestServiceQuadlet_NeverBindsBeyondLoopback(t *testing.T) {
	out := BindQuadletPorts("servlo-mysql", serviceQuadlet("127.0.0.1:3306:3306"))
	assertLoopbackOnly(t, out)
}

// A quadlet that arrives already bound to every interface, from an older
// install or an edit by hand, is pulled back to loopback rather than left.
func TestServiceQuadlet_PullsAPublicBindBackToLoopback(t *testing.T) {
	for _, publish := range []string{"3306:3306", "0.0.0.0:3306:3306", "[::]:3306:3306"} {
		out := BindQuadletPorts("servlo-mysql", serviceQuadlet(publish))
		assertLoopbackOnly(t, out)
	}
}

// nginx is the exception, and has to be: it serves the sites, so it binds
// every interface. There is no configuration that makes it stop, because a
// panel whose sites answer nobody is not serving anything.
func TestNginxQuadlet_AlwaysBindsEveryInterface(t *testing.T) {
	for _, publish := range []string{"127.0.0.1:80:80", "80:80"} {
		nginx := "[Container]\nImage=docker.io/library/nginx:alpine\nPublishPort=" + publish + "\n"
		out := BindQuadletPorts("servlo-nginx", nginx)
		if !strings.Contains(out, "PublishPort=80:80") {
			t.Errorf("nginx did not end up on every interface from %q:\n%s", publish, out)
		}
		if strings.Contains(out, "PublishPort=127.0.0.1:80:80") {
			t.Errorf("nginx stayed on loopback from %q, so the sites answer nobody:\n%s", publish, out)
		}
	}
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
