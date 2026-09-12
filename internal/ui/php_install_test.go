package ui

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func TestInstallablePHPVersions(t *testing.T) {
	supported := []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

	tests := []struct {
		name      string
		installed []string
		want      []string
	}{
		{
			name:      "none installed returns all in order",
			installed: nil,
			want:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
		},
		{
			name:      "filters installed and preserves order",
			installed: []string{"8.3", "7.4"},
			want:      []string{"8.0", "8.1", "8.2", "8.4", "8.5"},
		},
		{
			name:      "all installed returns empty non-nil slice",
			installed: supported,
			want:      []string{},
		},
		{
			name:      "unknown installed version is ignored",
			installed: []string{"5.6"},
			want:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := installablePHPVersions(supported, tc.installed)
			if got == nil {
				t.Fatal("expected non-nil slice")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("installablePHPVersions(%v) = %v, want %v", tc.installed, got, tc.want)
			}
		})
	}
}

// Removing a PHP version stops and deletes the container every pool on that
// version lives in, so a site still on it answers 502 from that moment. The
// route did it unconditionally, and config.CountSitesUsingPHP, written to
// answer exactly this question, had no caller anywhere in the tree.
func TestPHPVersionRemovalRefusal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	for _, s := range []config.Site{
		{Name: "live", Domains: []string{"live.example"}, Path: dir + "/live", PHPVersion: "8.4"},
		{Name: "paused", Domains: []string{"paused.example"}, Path: dir + "/paused", PHPVersion: "8.4", Paused: true},
		{Name: "elsewhere", Domains: []string{"other.example"}, Path: dir + "/other", PHPVersion: "8.3"},
	} {
		if err := config.AddSite(s); err != nil {
			t.Fatal(err)
		}
	}

	if why := phpVersionRemovalRefusal("8.4"); why == "" {
		t.Error("a version still running a site was removable, which takes that site down with no warning")
	} else if !strings.Contains(why, "8.4") || !strings.Contains(why, "1 site") {
		t.Errorf("the refusal does not say what is on it: %q", why)
	}

	// A paused site's container is already stopped, so it is not what the
	// refusal is protecting, and 8.2 has nothing on it at all.
	if why := phpVersionRemovalRefusal("8.2"); why != "" {
		t.Errorf("a version with nothing on it was refused: %q", why)
	}
}

// The decision above is only worth having if the route asks it. Gutting the
// call while leaving the function intact left the unit test above green, which
// is the gap this closes.
func TestPHPVersionRemoveRouteAsksBeforeTearingDown(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	i := strings.Index(body, `case "remove":`)
	if i < 0 {
		t.Fatal(`no "remove" case found in the PHP version route, so this check proved nothing`)
	}
	j := strings.Index(body[i:], `case "`+"ports"+`":`)
	if j < 0 {
		t.Fatal("could not find the end of the remove case")
	}
	remove := body[i : i+j]
	if !strings.Contains(remove, "phpVersionRemovalRefusal") {
		t.Error("the remove case tears the version down without asking whether any site is on it")
	}
	if strings.Index(remove, "phpVersionRemovalRefusal") > strings.Index(remove, "teardownPHPFPM") {
		t.Error("the refusal is checked after the teardown, which is after the sites are already down")
	}
}
