package podman

import (
	"strings"
	"testing"
)

const nginxPorts = "[Container]\nPublishPort=80:80\nPublishPort=443:443\nVolume=/a:/a\n"

func TestApplyHostPorts_MovesNginxOffThePrivilegedPorts(t *testing.T) {
	got := ApplyHostPorts(nginxPorts, 8080, 8443)
	if !strings.Contains(got, "PublishPort=8080:80") {
		t.Errorf("http not remapped:\n%s", got)
	}
	if !strings.Contains(got, "PublishPort=8443:443") {
		t.Errorf("https not remapped:\n%s", got)
	}
	// The container side is nginx's own listen directive and never moves.
	if strings.Contains(got, ":8080\n") || strings.Contains(got, ":8443\n") {
		t.Errorf("rewrote the container side:\n%s", got)
	}
	if !strings.Contains(got, "Volume=/a:/a") {
		t.Errorf("touched a line that is not a PublishPort:\n%s", got)
	}
}

func TestApplyHostPorts_LeavesTheDefaultsAlone(t *testing.T) {
	if got := ApplyHostPorts(nginxPorts, 80, 443); got != nginxPorts {
		t.Errorf("rewrote an already-correct quadlet:\n%s", got)
	}
}

// An address-qualified bind keeps its address: the host port is the
// second-to-last segment, not the first.
func TestApplyHostPorts_PreservesABindAddress(t *testing.T) {
	got := ApplyHostPorts("PublishPort=127.0.0.1:80:80\n", 8080, 8443)
	if !strings.Contains(got, "PublishPort=127.0.0.1:8080:80") {
		t.Errorf("lost the bind address:\n%s", got)
	}
}

// A service publishing its own port that happens not to be 80 or 443 must not
// be dragged along by the nginx strategy.
func TestApplyHostPorts_IgnoresUnrelatedPorts(t *testing.T) {
	in := "PublishPort=3306:3306\n"
	if got := ApplyHostPorts(in, 8080, 8443); got != in {
		t.Errorf("rewrote an unrelated publish: %q", got)
	}
}

func TestApplyHostPorts_ZeroPortsAreANoOp(t *testing.T) {
	if got := ApplyHostPorts(nginxPorts, 0, 0); got != nginxPorts {
		t.Errorf("acted on an unset strategy:\n%s", got)
	}
}
