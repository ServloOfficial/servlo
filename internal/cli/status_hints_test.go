package cli

import (
	"strings"
	"testing"
)

func TestServiceStartHint_containsUnit(t *testing.T) {
	hint := serviceStartHint("servlo-nginx")
	if !strings.Contains(hint, "servlo-nginx") && !strings.Contains(hint, "servlo start") {
		t.Errorf("serviceStartHint should reference the unit or servlo start, got %q", hint)
	}
}

func TestServiceStatusHint_containsUnit(t *testing.T) {
	hint := serviceStatusHint("servlo-nginx")
	if !strings.Contains(hint, "servlo-nginx") && !strings.Contains(hint, "servlo start") {
		t.Errorf("serviceStatusHint should reference the unit or servlo start, got %q", hint)
	}
}

func TestDnsRestartHint_nonEmpty(t *testing.T) {
	hint := dnsRestartHint()
	if hint == "" {
		t.Error("dnsRestartHint should not be empty")
	}
}

func TestPodmanDaemonHint_nonEmpty(t *testing.T) {
	hint := podmanDaemonHint()
	if hint == "" {
		t.Error("podmanDaemonHint should not be empty")
	}
}
