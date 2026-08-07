package config

import "testing"

func prodEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// Off by default. Production mode changes how errors are shown and how workers
// restart, and turning that on for someone who has not asked is the kind of
// surprise that costs an afternoon of debugging a blank page.
func TestProductionMode_OffByDefault(t *testing.T) {
	prodEnv(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.ProductionMode() {
		t.Error("a fresh install is in production mode")
	}
}

func TestProductionMode_SurvivesARoundTrip(t *testing.T) {
	prodEnv(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProductionMode(true)
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	reloaded, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.ProductionMode() {
		t.Error("production mode did not survive a save and reload")
	}
	if reloaded.ProductionSince().IsZero() {
		t.Error("production mode records no time it was enabled")
	}
}

// Turning it off clears the timestamp too, so a machine that was briefly in
// production mode months ago does not still claim a start date.
func TestProductionMode_DisablingClearsTheTimestamp(t *testing.T) {
	prodEnv(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProductionMode(true)
	cfg.SetProductionMode(false)

	if cfg.ProductionMode() {
		t.Error("production mode stayed on after being turned off")
	}
	if !cfg.ProductionSince().IsZero() {
		t.Error("the enabled-at timestamp survived being turned off")
	}
}

// Enabling twice must not move the timestamp. How long a machine has been in
// production is worth knowing, and re-running the command should not reset it.
func TestProductionMode_ReenablingKeepsTheOriginalTime(t *testing.T) {
	prodEnv(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProductionMode(true)
	first := cfg.ProductionSince()
	cfg.SetProductionMode(true)

	if !cfg.ProductionSince().Equal(first) {
		t.Errorf("the enabled-at timestamp moved from %v to %v on a second enable", first, cfg.ProductionSince())
	}
}

// A nil receiver reads as not-production. Every other predicate in this package
// treats an unreadable config as the safe default, and here the safe default is
// the one that does not silently hide errors from someone debugging.
func TestProductionMode_NilConfigIsNotProduction(t *testing.T) {
	var cfg *GlobalConfig
	if cfg.ProductionMode() {
		t.Error("a nil config reported production mode")
	}
	if !cfg.ProductionSince().IsZero() {
		t.Error("a nil config reported a production start time")
	}
}
