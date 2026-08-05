package siteops

import (
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func TestResolveSecured(t *testing.T) {
	securedProj := &config.ProjectConfig{Secured: true}
	plainProj := &config.ProjectConfig{Secured: false}

	cases := []struct {
		name   string
		relink bool
		proj   *config.ProjectConfig
		want   bool
	}{
		{"secured project", false, securedProj, true},
		{"plain project", false, plainProj, false},
		{"no .servlo.yaml", false, nil, false},
		{"re-link preserves prior secured", true, plainProj, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveSecured(tc.relink, tc.proj); got != tc.want {
				t.Errorf("ResolveSecured(%v, %+v) = %v, want %v", tc.relink, tc.proj, got, tc.want)
			}
		})
	}
}
