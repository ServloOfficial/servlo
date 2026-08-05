package config

import "testing"

// A definition servlo already has must not change under a running site without
// the operator asking. It carries the deploy commands and the worker set, so a
// silent refresh can alter what happens on the next deploy of a site nobody
// touched.
func TestShouldRefetchDefinition_PinnedByDefault(t *testing.T) {
	if shouldRefetchDefinition(true, false) {
		t.Error("refetched a definition already on disk with refresh off")
	}
}

// Missing is different from stale: with nothing on disk there is no pin to
// honour and no definition to serve, so it is always fetched.
func TestShouldRefetchDefinition_AlwaysFetchesWhatIsMissing(t *testing.T) {
	if !shouldRefetchDefinition(false, false) {
		t.Error("did not fetch a definition that is not on disk")
	}
	if !shouldRefetchDefinition(false, true) {
		t.Error("did not fetch a missing definition with refresh on")
	}
}

// Opting in is what moves a pinned install forward.
func TestShouldRefetchDefinition_RefreshOptsIn(t *testing.T) {
	if !shouldRefetchDefinition(true, true) {
		t.Error("refresh was enabled and the definition was still not refetched")
	}
}

func TestStoreAutoRefresh_DefaultsToPinned(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.StoreAutoRefresh() {
		t.Error("a fresh config auto-refreshes store definitions; it should pin them")
	}
}

func TestStoreAutoRefresh_HonoursAnExplicitOptIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	on := true
	cfg.Stores.AutoRefresh = &on
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	reloaded, err := LoadGlobal()
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if !reloaded.StoreAutoRefresh() {
		t.Error("an explicit opt-in did not survive a round trip")
	}
}
