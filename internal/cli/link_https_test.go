package cli

import "testing"

func TestShouldSecureOnLink(t *testing.T) {
	cases := []struct {
		name                     string
		projSecured, siteSecured bool
		want                     bool
	}{
		{"a project asking for HTTPS gets it", true, false, true},
		{"already secured needs nothing", true, true, false},
		// The bug: an absent .servlo.yaml reads as secured:false, and treating
		// that as intent dropped a secured site to HTTP on every re-link.
		{"a project with no opinion never turns HTTPS off", false, true, false},
		{"no opinion and nothing set stays put", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldSecureOnLink(c.projSecured, c.siteSecured); got != c.want {
				t.Errorf("shouldSecureOnLink(%v, %v) = %v, want %v",
					c.projSecured, c.siteSecured, got, c.want)
			}
		})
	}
}
