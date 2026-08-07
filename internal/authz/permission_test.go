package authz

import (
	"strings"
	"testing"
)

// Every permission a route can declare has to be one this package knows. A
// typo'd permission that fell through to "allowed" would be an access grant
// spelled as a mistake.
func TestPermission_OnlyKnownValuesAreValid(t *testing.T) {
	for _, p := range AllPermissions() {
		if !p.Valid() {
			t.Errorf("%q is in AllPermissions but not valid", p)
		}
	}
	for _, p := range []Permission{"", "admin ", "Admin", "site:write ", "invented"} {
		if p.Valid() {
			t.Errorf("%q is valid", p)
		}
	}
}

// The registry is the single place a route's authority is written down. A
// route missing from it is the thing the surface scan fails the build on.
func TestPermissionRegistry_LooksUpByLongestPrefix(t *testing.T) {
	registry := Permissions()

	for _, tc := range []struct {
		path string
		want Permission
	}{
		{"/api/sites", PermSiteList},
		{"/api/sites/example.com", PermSite},
		{"/api/sites/example.com/deploy", PermSite},
		{"/api/app-logs/example.com/laravel.log", PermSite},
		{"/api/services", PermAdmin},
		{"/api/servlo/stop", PermAdmin},
		{"/api/auth/login", PermPublic},
		{"/api/auth/totp/enrol", PermSelf},
	} {
		got, ok := registry.For(tc.path)
		if !ok {
			t.Errorf("%s is not declared", tc.path)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// A path nobody declared is undeclared, not permitted. This is the property
// the scan rests on: if lookup invented an answer, the scan could never tell
// a declared route from an undeclared one.
func TestPermissionRegistry_UndeclaredPathsAreNotFound(t *testing.T) {
	registry := Permissions()
	for _, path := range []string{"/api/invented-later", "/api/", "/nonsense", ""} {
		if _, ok := registry.For(path); ok {
			t.Errorf("%q resolved to a permission", path)
		}
	}
}

// Every entry in the registry declares a permission this package knows, or the
// registry is where a typo turns into an access grant.
func TestPermissionRegistry_EveryEntryIsValid(t *testing.T) {
	for pattern, permission := range Permissions() {
		if !permission.Valid() {
			t.Errorf("%s declares %q, which is not a permission", pattern, permission)
		}
		if !strings.HasPrefix(pattern, "/") {
			t.Errorf("%q is not a path", pattern)
		}
	}
}

// The scope check reads the registry rather than a second list of its own.
// Two lists is how they drift, and the one that drifts is always the one doing
// the enforcing.
func TestPermissionRegistry_DrivesTheScopeCheck(t *testing.T) {
	developer := Scope{Role: RoleDeveloper, Sites: []string{"example.com"}}
	admin := Scope{Role: RoleAdmin}

	for _, tc := range []struct {
		path             string
		devMay, adminMay bool
	}{
		{"/api/sites", true, true},
		{"/api/sites/example.com/deploy", true, true},
		{"/api/sites/other.com/deploy", false, true},
		{"/api/services/mysql/start", false, true},
		{"/api/servlo/stop", false, true},
		{"/api/auth/totp/enrol", true, true},
		{"/api/invented-later", false, true},
	} {
		if got := developer.allows(tc.path); got != tc.devMay {
			t.Errorf("developer on %s = %v, want %v", tc.path, got, tc.devMay)
		}
		if got := admin.allows(tc.path); got != tc.adminMay {
			t.Errorf("admin on %s = %v, want %v", tc.path, got, tc.adminMay)
		}
	}
}

// A site-scoped route names its site as the first segment after the prefix.
// That is the only shape the check can read a domain out of, so a PermSite
// entry that is not a prefix pattern would be a declaration servlo cannot act
// on: the check would have nothing to compare the scope against.
func TestPermissionRegistry_SiteScopedEntriesArePrefixPatterns(t *testing.T) {
	for pattern, permission := range Permissions() {
		if permission != PermSite {
			continue
		}
		if !strings.HasSuffix(pattern, "/") {
			t.Errorf("%s declares %q but is an exact pattern, so no domain follows it", pattern, permission)
		}
	}
}

// The domain is read from what follows the matched pattern, so a route whose
// prefix is not /api/sites/ scopes correctly too.
func TestScope_ScopesEveryPermSiteRouteNotJustTheSitesTree(t *testing.T) {
	developer := Scope{Role: RoleDeveloper, Sites: []string{"example.com"}}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/api/app-logs/example.com/laravel.log", true},
		{"/api/app-logs/other.com/laravel.log", false},
		{"/api/sites/example.com/deploy", true},
		{"/api/sites/other.com/deploy", false},
		// A site route with no site in it has nothing to check against.
		{"/api/app-logs/", false},
	} {
		if got := developer.allows(tc.path); got != tc.want {
			t.Errorf("developer on %s = %v, want %v", tc.path, got, tc.want)
		}
	}
}
