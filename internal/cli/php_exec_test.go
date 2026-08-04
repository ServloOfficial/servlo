package cli

import (
	"reflect"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func TestPhpScriptArgIndex(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"bare script", []string{"artisan", "migrate"}, 0},
		{"absolute script", []string{"/tmp/ide-phpinfo.php"}, 0},
		{"glued -d before script", []string{"-dmemory_limit=-1", "/tmp/x.php"}, 1},
		{"separated -d before script", []string{"-d", "memory_limit=-1", "script.php"}, 2},
		{"script then data file arg", []string{"check.php", "/tmp/input.xml"}, 0},
		{"-r runs no file", []string{"-r", "echo 1;"}, -1},
		{"-v runs no file", []string{"-v"}, -1},
		{"-f names the script", []string{"-f", "/tmp/x.php"}, 1},
		{"empty", nil, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := phpScriptArgIndex(tt.args); got != tt.want {
				t.Errorf("phpScriptArgIndex(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

// A CLI exec must tag the container with the registered site name so debug
// events (queries included) resolve to a site rather than falling back to the
// working-directory basename, or to nothing at all (#1005).
func TestDebugSiteEnvArgs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	if err := config.AddSite(config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    dir,
	}); err != nil {
		t.Fatal(err)
	}
	got := debugSiteEnvArgs(dir)
	want := []string{"--env", "SERVLO_SITE=rapids"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if got := debugSiteEnvArgs(t.TempDir()); got != nil {
		t.Errorf("unregistered dir args = %v, want none", got)
	}
}
