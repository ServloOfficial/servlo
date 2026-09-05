package ui

import (
	"encoding/json"
	"testing"

	"github.com/ServloOfficial/servlo/internal/authz"
)

// The websocket pushes a snapshot of every site to every connection. Gating
// the routes and leaving that alone would mean a developer never able to open
// another team's site but able to watch it, which is the door beside the door
// S5.2 already closed once.
func TestSnapshotScope_DeveloperSeesOnlyTheirSites(t *testing.T) {
	sites := []byte(`[{"domain":"example.com","php":"8.4"},{"domain":"other.com","php":"8.3"}]`)
	scope := authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}}

	filtered := scopeSites(sites, scope)
	var got []map[string]any
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatalf("the filtered snapshot is not valid JSON: %v (%s)", err, filtered)
	}
	if len(got) != 1 || got[0]["domain"] != "example.com" {
		t.Fatalf("filtered sites = %s, want only example.com", filtered)
	}
}

func TestSnapshotScope_AdminSeesEverySite(t *testing.T) {
	sites := []byte(`[{"domain":"example.com"},{"domain":"other.com"}]`)
	filtered := scopeSites(sites, authz.Scope{Role: authz.RoleAdmin})
	if string(filtered) != string(sites) {
		t.Errorf("an admin's snapshot was filtered: %s", filtered)
	}
}

// A developer with nothing assigned sees an empty list, not every site and not
// a broken frame.
func TestSnapshotScope_DeveloperWithNoSitesSeesAnEmptyList(t *testing.T) {
	sites := []byte(`[{"domain":"example.com"},{"domain":"other.com"}]`)
	filtered := scopeSites(sites, authz.Scope{Role: authz.RoleDeveloper})

	var got []map[string]any
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatalf("not valid JSON: %v (%s)", err, filtered)
	}
	if len(got) != 0 {
		t.Errorf("filtered sites = %s, want an empty list", filtered)
	}
}

// A snapshot servlo cannot parse is not one to pass through unfiltered. That
// would make a malformed payload the way to see everything.
func TestSnapshotScope_UnparseableSnapshotBecomesEmpty(t *testing.T) {
	for _, payload := range []string{`{"not":"an array"}`, `[`, `garbage`} {
		filtered := scopeSites([]byte(payload), authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}})
		if string(filtered) != "[]" {
			t.Errorf("an unparseable snapshot became %s, want an empty list", filtered)
		}
	}
}

// Services belong to the admin. A developer's frame carries none rather than
// carrying them and trusting the browser not to render them.
func TestSnapshotScope_DeveloperSeesNoServices(t *testing.T) {
	services := []byte(`[{"name":"mysql"},{"name":"redis"}]`)
	if got := scopeServices(services, authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}}); string(got) != "[]" {
		t.Errorf("a developer's frame carries services: %s", got)
	}
	if got := scopeServices(services, authz.Scope{Role: authz.RoleAdmin}); string(got) != string(services) {
		t.Errorf("an admin's services were filtered: %s", got)
	}
}

// Empty stays empty. A frame that carries no sites at all must not gain an
// empty array, because the client reads a missing key as "unchanged" and an
// empty one as "there are none".
func TestSnapshotScope_EmptyPayloadStaysEmpty(t *testing.T) {
	if got := scopeSites(nil, authz.Scope{Role: authz.RoleDeveloper}); len(got) != 0 {
		t.Errorf("an absent sites payload became %s", got)
	}
	if got := scopeServices(nil, authz.Scope{Role: authz.RoleDeveloper}); len(got) != 0 {
		t.Errorf("an absent services payload became %s", got)
	}
}
