package cli

import (
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func setProduction(t *testing.T, on bool) {
	t.Helper()
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProductionMode(on)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
}

// on-failure does not respawn a worker that exited cleanly, and a queue worker
// exiting 0 is the ordinary case: a graceful restart, a memory limit and
// Laravel's own --max-jobs all do it. On a live machine that means queued work
// silently stops being processed.
func TestResolveWorkerRestart_ProductionOverridesOnFailure(t *testing.T) {
	productionEnv(t)
	setProduction(t, true)

	if got := resolveWorkerRestart("on-failure"); got != "always" {
		t.Errorf("restart = %q in production, want always", got)
	}
}

// Outside production the framework's declaration is honoured. A store that says
// on-failure for a one-shot-ish worker has a reason, and overriding it on a
// development machine would restart-loop something the author did not intend to
// keep running.
func TestResolveWorkerRestart_DevelopmentHonoursTheStore(t *testing.T) {
	productionEnv(t)
	setProduction(t, false)

	if got := resolveWorkerRestart("on-failure"); got != "on-failure" {
		t.Errorf("restart = %q outside production, want the store's value", got)
	}
}

// An undeclared policy is always, in either mode. That was the behaviour before
// production mode existed and nothing about the mode should change it.
func TestResolveWorkerRestart_UndeclaredIsAlwaysInBothModes(t *testing.T) {
	productionEnv(t)
	for _, production := range []bool{false, true} {
		setProduction(t, production)
		if got := resolveWorkerRestart(""); got != "always" {
			t.Errorf("an undeclared policy resolved to %q (production=%v), want always", got, production)
		}
	}
}
