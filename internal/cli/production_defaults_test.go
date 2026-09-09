package cli

import (
	"os"
	"path/filepath"
	"strings"
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

// The unit written on every start has to carry the production policy too.
//
// resolveWorkerRestart is only consulted by `servlo worker start` and `worker
// add`. The restore path that `servlo start` runs, which is also every boot and
// the `servlo restart` the production command tells the operator to run, wrote
// the framework's declared policy verbatim. So turning production mode on and
// applying it as instructed put the queue workers back on on-failure, and a
// reboot did the same to a machine nobody had touched.
func TestRestoreWorker_WritesTheProductionRestartPolicy(t *testing.T) {
	productionEnv(t)
	setProduction(t, true)

	site := t.TempDir()
	restoreWorker("shop", site, "8.4", "queue", config.FrameworkWorker{
		Label:   "queue",
		Command: "php artisan queue:work",
		Restart: "on-failure",
	})

	unit := filepath.Join(config.SystemdUserDir(), WorkerUnitName("shop", site, "queue")+".service")
	body, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("reading the restored worker unit: %v", err)
	}
	if !strings.Contains(string(body), "Restart=always") {
		t.Errorf("the unit servlo start writes says:\n%s\nand in production a worker that exits cleanly has to come back", body)
	}
}

// And outside production the store's declaration still reaches the unit, the
// same way resolveWorkerRestart honours it.
func TestRestoreWorker_HonoursTheStoreOutsideProduction(t *testing.T) {
	productionEnv(t)
	setProduction(t, false)

	site := t.TempDir()
	restoreWorker("shop", site, "8.4", "queue", config.FrameworkWorker{
		Label:   "queue",
		Command: "php artisan queue:work",
		Restart: "on-failure",
	})

	unit := filepath.Join(config.SystemdUserDir(), WorkerUnitName("shop", site, "queue")+".service")
	body, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("reading the restored worker unit: %v", err)
	}
	if !strings.Contains(string(body), "Restart=on-failure") {
		t.Errorf("the unit says:\n%s\nand outside production the store's policy is the one that applies", body)
	}
}
