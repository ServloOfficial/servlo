package nginx

import (
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// A subdomain is a site like any other: its own directory, its own PHP
// version, its own certificate. It gets its own vhost, and nginx prefers an
// exact server_name over the parent's wildcard, so the subdomain's own vhost is
// the one that answers.
func TestSubdomain_GetsItsOwnVhostThatBeatsTheParent(t *testing.T) {
	parent := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
	}
	child := config.Site{
		Name: "admin-shop", Domains: []string{"admin.shop.example"},
		Path: "/home/u/admin", PHPVersion: "8.3", PublicDir: "public",
	}

	parentNames := serverNamesWithWildcards(parent.Domains)
	childNames := serverNamesWithWildcards(child.Domains)

	// The parent claims the wildcard, which is what routes an unregistered
	// subdomain to it.
	if !strings.Contains(parentNames, "*.shop.example") {
		t.Fatalf("parent server_name = %q", parentNames)
	}
	// The child names the subdomain exactly, and nginx resolves an exact
	// server_name before any wildcard, so this vhost wins for that host.
	if !strings.Contains(childNames, "admin.shop.example") {
		t.Errorf("child server_name = %q, does not name the subdomain exactly", childNames)
	}
	if strings.Contains(childNames, "*.shop.example") {
		t.Errorf("child server_name = %q, claims its parent's wildcard", childNames)
	}
}

// Each site's vhost is rendered from its own settings, so a subdomain running a
// different PHP version and a different document root is not sharing anything
// with the site it sits under.
func TestSubdomain_KeepsItsOwnRuntimeAndRoot(t *testing.T) {
	child := config.Site{
		Name: "admin-shop", Domains: []string{"admin.shop.example"},
		Path: "/home/u/admin", PHPVersion: "8.3", PublicDir: "web",
	}
	d := vhostDataFor(child)
	d.FPMContainer = "servlo-php83-fpm"

	out := renderTemplate(t, "vhost.conf.tmpl", d)

	if !strings.Contains(out, "/home/u/admin/web") {
		t.Errorf("the subdomain does not serve its own document root:\n%s", out)
	}
	if !strings.Contains(out, "servlo-php83-fpm") {
		t.Errorf("the subdomain does not use its own PHP version:\n%s", out)
	}
}
