package nginx

import (
	"strings"
	"testing"
)

// A framework snippet that hardcodes "{{fpm}}:9000" sends its own locations to
// the shared container while the rest of the site goes to the site's pool, so
// those requests get the version's settings instead of the site's. The
// placeholder that resolves the whole target is what lets a definition stop
// caring which of the two it is.
func TestExpandNginxSnippet_FastcgiPassFollowsTheSiteToItsPool(t *testing.T) {
	got, err := expandNginxSnippet(
		"location /setup {\n    {{fastcgi_pass}}\n}",
		"/home/u/shop", "pub",
		Upstream{Socket: "/home/u/.local/share/servlo/run/fpm/shop.sock", Container: "servlo-php84-fpm"},
	)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}

	if !strings.Contains(got, "fastcgi_pass unix:/home/u/.local/share/servlo/run/fpm/shop.sock;") {
		t.Errorf("the snippet does not pass to the site's own socket:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unexpanded placeholder in:\n%s", got)
	}
}

// A site with no pool of its own keeps the shared container, and reaching it by
// a literal name is what nginx resolves once and caches for the life of the
// worker. The variable indirection the main vhost uses has to come with the
// placeholder or a container restart leaves the snippet's locations on a dead
// address.
func TestExpandNginxSnippet_FastcgiPassKeepsTheContainerResolvable(t *testing.T) {
	got, err := expandNginxSnippet(
		"location /setup {\n    {{fastcgi_pass}}\n}",
		"/home/u/shop", "pub",
		Upstream{Container: "servlo-php84-fpm"},
	)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}

	if !strings.Contains(got, `"servlo-php84-fpm"`) {
		t.Errorf("the container is not named:\n%s", got)
	}
	// A literal upstream here is the bug: nginx caches it and the site 502s for
	// the worker's lifetime after the container comes back on a new address.
	if strings.Contains(got, "fastcgi_pass servlo-php84-fpm:9000;") {
		t.Errorf("the container is passed to literally, which nginx resolves once and caches:\n%s", got)
	}
	if !strings.Contains(got, "fastcgi_pass $") {
		t.Errorf("the pass target is not a variable:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unexpanded placeholder in:\n%s", got)
	}
}

// The socket path reaches this from a site handle, and a value carrying a
// semicolon or a brace would end the directive it lands in and open one the
// definition did not write.
func TestExpandNginxSnippet_RefusesAPassTargetThatWouldBreakOut(t *testing.T) {
	for _, up := range []Upstream{
		{Socket: "/run/fpm/x.sock;deny all"},
		{Container: "servlo-php84-fpm}\nlocation / {"},
		{Socket: "/run/fpm/x.sock # "},
	} {
		if _, err := expandNginxSnippet("{{fastcgi_pass}}", "/p", "pub", up); err == nil {
			t.Errorf("upstream %+v was accepted", up)
		}
	}
}
