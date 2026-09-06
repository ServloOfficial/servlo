package nginx

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A site answers for the domains it has and nothing else.
//
// It used to claim a *.domain wildcard for each of them, so an unregistered
// subdomain was served by its parent. That is convenient and wrong on a real
// server: the parent's certificate does not name the subdomain, so a visitor
// reaching it over HTTPS meets a browser security warning served by a site
// nobody meant to put there. Unregistered now means unregistered, and the
// request falls through to the default vhost.
func TestSubdomain_ParentDoesNotClaimSubdomainsItWasNotGiven(t *testing.T) {
	parent := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
	}
	child := config.Site{
		Name: "admin-shop", Domains: []string{"admin.shop.example"},
		Path: "/home/u/admin", PHPVersion: "8.3", PublicDir: "public",
	}

	parentNames := serverNames(parent.Domains)
	childNames := serverNames(child.Domains)

	if parentNames != "shop.example" {
		t.Errorf("parent server_name = %q, want only the domain it has", parentNames)
	}
	if childNames != "admin.shop.example" {
		t.Errorf("child server_name = %q, want only the domain it has", childNames)
	}
	// The specific thing that used to happen: the parent answering for a
	// subdomain it was never given.
	if strings.Contains(parentNames, "admin.shop.example") || strings.Contains(parentNames, "*") {
		t.Errorf("the parent still claims subdomains: %q", parentNames)
	}
}

// Aliases are still listed in full: a site answers for every domain it was
// given, and only those.
func TestSubdomain_EveryDomainTheSiteHasIsServed(t *testing.T) {
	site := config.Site{Domains: []string{"shop.example", "www.shop.example", "shop.co"}}

	got := serverNames(site.Domains)

	for _, want := range site.Domains {
		if !strings.Contains(got, want) {
			t.Errorf("server_name = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "*") {
		t.Errorf("server_name = %q, still carries a wildcard", got)
	}
}

// A site that genuinely wants every subdomain says so, by adding the wildcard
// as a domain of its own. That needs a wildcard certificate, which servlo can
// only issue over DNS-01, and both of those are the operator's decision rather
// than something applied to every site by default.
func TestSubdomain_AWildcardIsHonouredWhenTheSiteAsksForOne(t *testing.T) {
	site := config.Site{Domains: []string{"shop.example", "*.shop.example"}}

	got := serverNames(site.Domains)

	if !strings.Contains(got, "*.shop.example") {
		t.Errorf("server_name = %q, dropped the wildcard the site asked for", got)
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
