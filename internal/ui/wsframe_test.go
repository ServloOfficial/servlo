package ui

import "testing"

// isLocalNetworkHost gates the same-origin fallback in wsOriginAllowed against
// DNS rebinding: an IP literal can't be rebound, and only the reserved local
// TLDs (.localhost/.test) and mDNS .local are accepted for hostnames.
func TestIsLocalNetworkHost(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"localhost:7073", true},
		{"127.0.0.1:7073", true},
		{"[::1]:7073", true},
		{"192.168.1.50:7073", true},
		{"10.0.0.5", true},
		{"lerd.localhost", true},
		{"myapp.test", true},
		{"foo.local:7073", true},
		{"evil.example", false},
		{"attacker.com:7073", false},
		{"localhostx.evil.com", false},
		{"nottest.eviltest", false},
	}
	for _, tc := range cases {
		if got := isLocalNetworkHost(tc.in); got != tc.want {
			t.Errorf("isLocalNetworkHost(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
