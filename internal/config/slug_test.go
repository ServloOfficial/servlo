package config

import "testing"

func TestSiteSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"my-app", "my_app"},
		{"My-App", "my_app"},
		{"feat-x", "feat_x"},
		{"feat.acme.test", "feat_acme_test"},
		{"already_underscored", "already_underscored"},
		{"UPPER-CASE", "upper_case"},
		{"no-change", "no_change"},
		{"mixed.dots-and-hyphens", "mixed_dots_and_hyphens"},
		{"simple", "simple"},
		{"", ""},
		// Security: shell/SQL metacharacters must be neutralised so a hostile
		// directory or branch name cannot reach a site_init `sh -c` or a
		// CREATE DATABASE identifier carrying an injection payload. They map to
		// underscores, so the output is always [a-z0-9_].
		{"app`id`x", "app_id_x"},
		{`app"x`, "app_x"},
		{"a b;c", "a_b_c"},
		{"feature/x", "feature_x"},
		{"drop`whoami`", "drop_whoami_"},
		{`x";DROP`, "x__drop"},
	}
	for _, tc := range cases {
		if got := SiteSlug(tc.in); got != tc.want {
			t.Errorf("SiteSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
