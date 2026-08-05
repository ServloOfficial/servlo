package cli

import (
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/ports"
)

// The recorded strategy is what doctor checks against later, so an install that
// applies one and records another leaves doctor verifying the wrong thing.
func TestApplyPortStrategyRecordsTheChoiceAndItsPorts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := applyPortStrategy(); err != nil {
		t.Fatalf("applyPortStrategy: %v", err)
	}

	saved, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("reloading config: %v", err)
	}

	strategy := ports.ParseStrategy(saved.PortStrategy())
	wantHTTP, wantHTTPS := ports.Publish(strategy)
	if saved.Nginx.HTTPPort != wantHTTP || saved.Nginx.HTTPSPort != wantHTTPS {
		t.Errorf("recorded %s with ports %d/%d, want %d/%d",
			strategy, saved.Nginx.HTTPPort, saved.Nginx.HTTPSPort, wantHTTP, wantHTTPS)
	}
	if saved.PortStrategy() == "" {
		t.Error("no strategy recorded, so doctor has nothing to verify against")
	}
}
