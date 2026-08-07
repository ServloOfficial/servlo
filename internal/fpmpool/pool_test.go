package fpmpool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settings() Settings {
	return Settings{
		Site:      "example-com",
		Root:      "/home/servlo/sites/example.com",
		SocketDir: "/home/servlo/.local/share/servlo/run/fpm",
	}
}

func render(t *testing.T, s Settings) string {
	t.Helper()
	out, err := Render(s)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

func TestRender_GivesTheSiteItsOwnPoolAndSocket(t *testing.T) {
	out := render(t, settings())

	if !strings.Contains(out, "[example-com]") {
		t.Errorf("no pool section named for the site:\n%s", out)
	}
	// Its own socket is what makes the pool reachable independently, and what
	// lets nginx send this site's requests to this site's settings.
	if !strings.Contains(out, "listen = /home/servlo/.local/share/servlo/run/fpm/example-com.sock") {
		t.Errorf("the pool does not listen on its own socket:\n%s", out)
	}
}

// nginx runs in its own container as the same user. The socket has to be
// reachable from there and from nowhere else that matters.
func TestRender_MakesTheSocketReachableByNginxAndNoWiderThanThat(t *testing.T) {
	out := render(t, settings())

	if !strings.Contains(out, "listen.mode = 0660") {
		t.Errorf("the socket mode is not 0660:\n%s", out)
	}
}

// The point of the story: PHP settings are per site, so they belong in the
// pool rather than in an ini shared by every site on the version.
func TestRender_WritesThePerSiteSettingsIntoThePool(t *testing.T) {
	s := settings()
	s.MaxUploadMB = 64
	s.MaxExecutionSeconds = 120
	s.MemoryLimitMB = 512

	out := render(t, s)

	for _, want := range []string{
		"php_admin_value[upload_max_filesize] = 64M",
		// post_max_size travels with upload_max_filesize or the larger upload
		// is refused by the other half of the pair.
		"php_admin_value[post_max_size] = 64M",
		"php_admin_value[max_execution_time] = 120",
		"php_admin_value[memory_limit] = 512M",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// Never expose half of one of these pairs (CLAUDE.md §3.4). One field has to
// write both PHP directives, or an operator raises the upload limit and the
// upload still fails.
func TestRender_UploadSizeAlwaysWritesBothHalvesOfThePair(t *testing.T) {
	s := settings()
	s.MaxUploadMB = 128

	out := render(t, s)

	upload := strings.Count(out, "upload_max_filesize] = 128M")
	post := strings.Count(out, "post_max_size] = 128M")
	if upload != 1 || post != 1 {
		t.Errorf("upload appears %d times and post %d, want one each:\n%s", upload, post, out)
	}
}

// A site that sets nothing gets the production defaults and no per-site
// overrides, rather than a pool restating values that already come from the
// shared production ini.
func TestRender_LeavesUnsetSettingsToTheDefaults(t *testing.T) {
	out := render(t, settings())

	for _, unwanted := range []string{"upload_max_filesize", "max_execution_time", "memory_limit"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("an unset setting was written anyway (%s):\n%s", unwanted, out)
		}
	}
}

// Production defaults every pool carries regardless (CLAUDE.md §3.4), because a
// site is not the place to turn these back on.
func TestRender_PinsTheProductionSafetySettings(t *testing.T) {
	out := render(t, settings())

	for _, want := range []string{
		"php_admin_value[display_errors] = Off",
		"php_admin_value[expose_php] = Off",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// admin_value rather than value: a site's own ini_set must not be able to
	// turn error display back on in front of visitors.
	if strings.Contains(out, "php_value[display_errors]") {
		t.Error("display_errors is overridable from the application")
	}
}

// A pool name and a socket path are built from the site handle, which reaches
// this from a registry a person edits.
func TestRender_RefusesASiteHandleThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"", "../evil", "a/b", "with space", "has\nnewline", "semi;colon"} {
		s := settings()
		s.Site = bad
		if _, err := Render(s); err == nil {
			t.Errorf("site handle %q was accepted", bad)
		}
	}
}

// The root is interpolated into the pool as chdir, and a newline there would
// end the directive and start another one of the archive's choosing.
func TestRender_RefusesARootThatWouldInjectADirective(t *testing.T) {
	for _, bad := range []string{"/srv/x\nphp_admin_value[open_basedir] = /", "relative/path", ""} {
		s := settings()
		s.Root = bad
		if _, err := Render(s); err == nil {
			t.Errorf("root %q was accepted", bad)
		}
	}
}

// A setting out of range is a mistake, and writing it produces a pool FPM
// refuses to start with, which takes down every site sharing the container.
func TestRender_RefusesSettingsOutOfRange(t *testing.T) {
	for _, mutate := range []func(*Settings){
		func(s *Settings) { s.MaxUploadMB = -1 },
		func(s *Settings) { s.MaxUploadMB = 100000 },
		func(s *Settings) { s.MaxExecutionSeconds = -5 },
		func(s *Settings) { s.MemoryLimitMB = -1 },
	} {
		s := settings()
		mutate(&s)
		if _, err := Render(s); err == nil {
			t.Errorf("out-of-range settings were accepted: %+v", s)
		}
	}
}

func TestWrite_PutsThePoolWhereFPMLooksForIt(t *testing.T) {
	dir := t.TempDir()
	s := settings()
	s.MaxUploadMB = 32

	path, err := Write(dir, s)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("wrote to %q, outside %q", path, dir)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "upload_max_filesize] = 32M") {
		t.Errorf("the written pool is missing its settings:\n%s", body)
	}
	// Rewriting has to replace rather than append, or a settings change leaves
	// two pools with the same name and FPM refuses to start.
	s.MaxUploadMB = 64
	if _, err := Write(dir, s); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	body, _ = os.ReadFile(path)
	if strings.Contains(string(body), "32M") {
		t.Errorf("the rewrite left the old value behind:\n%s", body)
	}
	if strings.Count(string(body), "[example-com]") != 1 {
		t.Errorf("the rewrite produced a duplicate pool:\n%s", body)
	}
}

func TestRemove_TakesThePoolAway(t *testing.T) {
	dir := t.TempDir()
	path, err := Write(dir, settings())
	if err != nil {
		t.Fatal(err)
	}

	if err := Remove(dir, "example-com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the pool survived Remove")
	}
	// Removing a pool that is not there is the ordinary state for a site that
	// never had one.
	if err := Remove(dir, "example-com"); err != nil {
		t.Errorf("removing an absent pool errored: %v", err)
	}
}

func TestRemove_RefusesAHandleThatIsAPath(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../../etc/passwd", "a/b", ""} {
		if err := Remove(dir, bad); err == nil {
			t.Errorf("Remove(%q) was accepted", bad)
		}
	}
}
