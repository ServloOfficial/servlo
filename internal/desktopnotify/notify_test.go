package desktopnotify

import "testing"

func TestClickURLs(t *testing.T) {
	cases := []struct{ route, app, pwa, browser string }{
		{"#system", "servlo://open/#system", "web+servlo://open/#system", "http://servlo.localhost/#system"},
		{"/sites/foo", "servlo://open/sites/foo", "web+servlo://open/sites/foo", "http://servlo.localhost/sites/foo"},
		{"", "servlo://open/", "web+servlo://open/", "http://servlo.localhost/"},
	}
	for _, tc := range cases {
		if got := appSchemeURL(tc.route); got != tc.app {
			t.Errorf("appSchemeURL(%q)=%q, want %q", tc.route, got, tc.app)
		}
		if got := pwaSchemeURL(tc.route); got != tc.pwa {
			t.Errorf("pwaSchemeURL(%q)=%q, want %q", tc.route, got, tc.pwa)
		}
		if got := browserURL(tc.route); got != tc.browser {
			t.Errorf("browserURL(%q)=%q, want %q", tc.route, got, tc.browser)
		}
	}
}

func TestUrgencyFromString(t *testing.T) {
	cases := map[string]Urgency{
		"":         UrgencyNormal,
		"normal":   UrgencyNormal,
		"low":      UrgencyLow,
		"critical": UrgencyCritical,
		"high":     UrgencyCritical,
		"weird":    UrgencyNormal,
	}
	for in, want := range cases {
		if got := UrgencyFromString(in); got != want {
			t.Errorf("UrgencyFromString(%q)=%d, want %d", in, got, want)
		}
	}
}
