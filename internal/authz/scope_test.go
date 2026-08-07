package authz

import "testing"

// An Admin reaches everything. That is the whole role, and stating it here
// stops a later scoping rule quietly narrowing it.
func TestScope_AdminReachesEverySite(t *testing.T) {
	scope := Scope{Role: RoleAdmin}
	for _, domain := range []string{"example.com", "anything.test", ""} {
		if !scope.MaySee(domain) {
			t.Errorf("an admin cannot see %q", domain)
		}
	}
	if !scope.MayAdminister() {
		t.Error("an admin cannot administer")
	}
}

// A Developer reaches the sites assigned to them and no others.
func TestScope_DeveloperReachesOnlyAssignedSites(t *testing.T) {
	scope := Scope{Role: RoleDeveloper, Sites: []string{"example.com", "shop.example.com"}}

	for _, domain := range []string{"example.com", "shop.example.com"} {
		if !scope.MaySee(domain) {
			t.Errorf("a developer cannot see their assigned site %q", domain)
		}
	}
	for _, domain := range []string{"other.com", "notexample.com", "example.com.evil.net", ""} {
		if scope.MaySee(domain) {
			t.Errorf("a developer can see %q, which is not theirs", domain)
		}
	}
}

// A developer with nothing assigned sees nothing, rather than everything. An
// empty list is the state right after an account is made, and reading it as
// "unrestricted" would make every new developer an admin for a moment.
func TestScope_DeveloperWithNoSitesSeesNothing(t *testing.T) {
	scope := Scope{Role: RoleDeveloper}
	if scope.MaySee("example.com") {
		t.Error("a developer with no assigned sites can see one")
	}
	if scope.MayAdminister() {
		t.Error("a developer can administer")
	}
}

// A role servlo does not recognise is a corrupt record, not a role with no
// permissions that happens to work everywhere.
func TestScope_UnknownRoleReachesNothing(t *testing.T) {
	scope := Scope{Role: Role("superuser"), Sites: []string{"example.com"}}
	if scope.MaySee("example.com") || scope.MayAdminister() {
		t.Error("an unrecognised role was granted something")
	}
}

// Domains are compared case-insensitively and without a trailing dot, because
// a URL can carry either and a site is the same site regardless.
func TestScope_MatchesDomainsTheWayDNSDoes(t *testing.T) {
	scope := Scope{Role: RoleDeveloper, Sites: []string{"Example.COM"}}
	for _, domain := range []string{"example.com", "EXAMPLE.COM", "example.com."} {
		if !scope.MaySee(domain) {
			t.Errorf("%q did not match the assigned Example.COM", domain)
		}
	}
}

// The list of sites a request may act on is what the panel filters by, so it
// has to say "all" distinctly from "these two".
func TestScope_ReportsWhetherItIsLimited(t *testing.T) {
	if (Scope{Role: RoleAdmin}).Limited() {
		t.Error("an admin's scope reports as limited")
	}
	if !(Scope{Role: RoleDeveloper, Sites: []string{"example.com"}}).Limited() {
		t.Error("a developer's scope does not report as limited")
	}
}
