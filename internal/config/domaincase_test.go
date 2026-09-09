package config

import "testing"

// A domain is case-insensitive, and servlo compares one byte for byte.
//
// DNS says SHOP.EXAMPLE and shop.example are the same name, nginx matches
// server_name that way, and a browser may send either. The registry did not:
// HasDomain was an exact string comparison, so a lookup that arrived in another
// case found nothing.
//
// It also has to agree with authz.Scope.MaySee, which already normalises case
// and the trailing dot when it decides whether a Developer may touch a site. The
// two disagreeing is only harmless while every entry point lowercases a domain
// before it is stored, which is true today and is not a thing to rely on: the
// permissive half deciding yes and the strict half then resolving a different
// site is how a scope check ends up guarding the wrong door.
func TestHasDomain_MatchesTheWayDNSDoes(t *testing.T) {
	site := &Site{Name: "shop", Domains: []string{"shop.example", "www.shop.example"}}

	for _, want := range []string{
		"shop.example",
		"SHOP.EXAMPLE",
		"Shop.Example",
		"shop.example.",
		"WWW.Shop.Example",
	} {
		if !site.HasDomain(want) {
			t.Errorf("HasDomain(%q) is false, and it is the same name as shop.example", want)
		}
	}

	for _, other := range []string{
		"", "shop.example.com", "notshop.example", "shop.exampl", "shop .example",
	} {
		if site.HasDomain(other) {
			t.Errorf("HasDomain(%q) is true, and it is a different name", other)
		}
	}
}
