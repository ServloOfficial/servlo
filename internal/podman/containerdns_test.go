package podman

import (
	"testing"
)

// Rescued from internal/dns when the .test stack went: the filtering it does is
// about what podman and netavark can consume, not about local DNS.
func TestSanitizeDNSIP(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"8.8.8.8", "8.8.8.8"},
		{"  1.1.1.1  ", "1.1.1.1"},
		{"169.254.1.1", "169.254.1.1"},
		{"2606:4700:4700::1111", "2606:4700:4700::1111"},
		{"127.0.0.1", ""},
		{"127.0.0.53", ""},
		{"::1", ""},
		{"0.0.0.0", ""},
		{"", ""},
		{"--", ""},
		{"not-an-ip", ""},
		{"fe80::46d4:53ff:fe3f:a9a7%18", ""},
		{"fe80::1%eth0", ""},
	}
	for _, c := range cases {
		if got := sanitizeDNSIP(c.in); got != c.want {
			t.Errorf("sanitizeDNSIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A network created with no DNS servers leaves its containers unable to resolve
// anything, so the list has to fall back rather than come back empty.
func TestContainerDNS_NeverEmpty(t *testing.T) {
	if got := ContainerDNS(); len(got) == 0 {
		t.Error("ContainerDNS() returned no servers")
	}
}
