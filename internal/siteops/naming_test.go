package siteops

import "testing"

func TestSiteName_TableOfDirectoryShapes(t *testing.T) {
	cases := []struct {
		dirName  string
		wantName string
	}{
		{"myapp", "myapp"},
		{"myapp.com", "myapp"},
		{"my-app.io", "my-app"},
		{"My-App.COM", "my-app"},
		{"foo.bar.baz", "foo-bar-baz"},
		{"example.co.uk", "example-co"}, // .uk stripped first
		{"plain", "plain"},
		{"dots.in.name", "dots-in-name"},

		// ccTLDs handled by the 2-letter regex, no enumeration needed.
		{"starlane.ro", "starlane"},
		{"mysite.nl", "mysite"},
		{"mysite.be", "mysite"},
		{"project.pl", "project"},

		// gTLDs from the curated list.
		{"shop.online", "shop"},
		{"studio.digital", "studio"},

		// Digit suffix must not be stripped; preserves version-style dir names.
		{"app.v2", "app-v2"},

		// Unknown longer suffix is left alone.
		{"backup.old", "backup-old"},

		// Characters that would inject a systemd directive or escape the unit
		// path are stripped so the handle is safe to use in unit names/bodies.
		{"app\nExecStartPre=evil", "appexecstartpre=evil"},
		{"a/b", "ab"},
		{"x\x00y", "xy"},
	}

	for _, c := range cases {
		if got := SiteName(c.dirName); got != c.wantName {
			t.Errorf("SiteName(%q) = %q, want %q", c.dirName, got, c.wantName)
		}
	}
}
