package cli

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// An assignment is a domain string, and nothing prunes it when the site holding
// that domain goes away. Listing them printed every entry the same way, so an
// assignment to a site that has not existed for months read exactly like one to
// a site serving now, and the admin deciding who reaches what had no way to
// tell them apart.
//
// This does not change who may reach what. It says which entries still name
// something on this server.
func TestAssignedSiteLines_MarksAnAssignmentNoSiteHolds(t *testing.T) {
	if err := config.AddSite(config.Site{
		Name: "live", Path: t.TempDir(), Domains: []string{"live.example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	lines := assignedSiteLines([]string{"live.example.com", "gone.example.com"})
	if len(lines) != 2 {
		t.Fatalf("expected one line per assignment, got %v", lines)
	}
	if !strings.Contains(lines[0], "live.example.com") || strings.Contains(lines[0], "no site") {
		t.Errorf("a live assignment was flagged: %q", lines[0])
	}
	if !strings.Contains(lines[1], "gone.example.com") {
		t.Errorf("the stale assignment lost its domain: %q", lines[1])
	}
	if !strings.Contains(lines[1], "no site on this server has this domain") {
		t.Errorf("a stale assignment read like a live one: %q", lines[1])
	}
}

// An alias is a domain the site really answers on, so an assignment to one is
// live. FindSiteByDomain resolves aliases, and this has to agree with it or it
// would flag a working assignment as dead.
func TestAssignedSiteLines_CountsAnAliasAsLive(t *testing.T) {
	if err := config.AddSite(config.Site{
		Name: "aliased", Path: t.TempDir(),
		Domains: []string{"primary.example.com", "alias.example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	lines := assignedSiteLines([]string{"alias.example.com"})
	if len(lines) != 1 || strings.Contains(lines[0], "no site") {
		t.Errorf("an alias assignment was flagged as holding no site: %v", lines)
	}
}
