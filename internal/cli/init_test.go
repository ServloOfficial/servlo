package cli

import "testing"

// TestPersistedSecured pins that the wizard persists the user's HTTPS intent to
// .lerd.yaml rather than the DNS-gated value: dropping a committed secured:true
// on a localhost box would silently strip HTTPS for teammates on a DNS box.
func TestPersistedSecured(t *testing.T) {
	cases := []struct {
		name           string
		chosen         bool
		httpsAvailable bool
		committed      bool
		want           bool
	}{
		{"dns on, user enabled", true, true, false, true},
		{"dns on, user declined", false, true, true, false},
		{"dns off keeps committed intent", false, false, true, true},
		{"dns off, never committed", false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := persistedSecured(tc.chosen, tc.httpsAvailable, tc.committed); got != tc.want {
				t.Errorf("persistedSecured(%v,%v,%v) = %v, want %v",
					tc.chosen, tc.httpsAvailable, tc.committed, got, tc.want)
			}
		})
	}
}
