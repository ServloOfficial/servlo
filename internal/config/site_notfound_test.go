package config

import (
	"errors"
	"testing"
)

// A caller has to be able to tell a site that is not registered from a registry
// that could not be read. Both used to arrive as a plain error, so a restore
// reported an unreadable registry as an archive belonging to another server.
func TestFindSite_ReportsAMissingSiteAsErrSiteNotFound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := FindSite("nothing-here")
	if err == nil {
		t.Fatal("looking up a site that is not registered reported success")
	}
	if !errors.Is(err, ErrSiteNotFound) {
		t.Errorf("error %v does not match ErrSiteNotFound", err)
	}
}
