package config

import (
	"strings"
	"testing"
)

// The server-state archive shares a directory with every site's archives and is
// found by name: servlo-state-<stamp>.servlobak, exactly the shape a site's own
// archive has. Retention prunes by that prefix, so a site called servlo-state
// would have its backups swept on the state policy rather than its own, and
// would list the machine's state archives as its own.
//
// Nobody is likely to name a site this. The cost of being wrong is somebody's
// backups deleted quietly, and the cost of the guard is a refusal at the one
// place a site enters the registry.
func TestAddSite_RefusesTheNameTheStateArchiveUses(t *testing.T) {
	secretEnv(t)

	err := AddSite(Site{Name: StateArchiveName, Domains: []string{"example.com"}, Path: t.TempDir()})
	if err == nil {
		t.Fatal("a site named after the state archive was accepted")
	}
	if !strings.Contains(err.Error(), StateArchiveName) {
		t.Errorf("the refusal does not name the problem: %v", err)
	}

	// And a name that merely starts with it is fine: the collision is the whole
	// name, because the stamp is what follows it.
	if err := AddSite(Site{Name: StateArchiveName + "ful", Domains: []string{"stateful.example"}, Path: t.TempDir()}); err != nil {
		t.Errorf("a site whose name only begins with it was refused: %v", err)
	}
}
