package cli

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/auditlog"
	"github.com/ServloOfficial/servlo/internal/config"
)

func productionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// Turning production mode off on a live machine starts showing PHP stack traces
// to the internet. A flag rather than a prompt, so it cannot be answered by
// muscle memory or by a script piping "y".
func TestProductionOff_RefusesWithoutForce(t *testing.T) {
	productionEnv(t)
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProductionMode(true)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	cmd := NewProductionCmd()
	cmd.SetArgs([]string{"off"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("production mode was turned off without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q does not say how to proceed", err)
	}

	reloaded, _ := config.LoadGlobal()
	if !reloaded.ProductionMode() {
		t.Error("the refused command still turned production mode off")
	}
}

func TestProductionOff_ForceTurnsItOff(t *testing.T) {
	productionEnv(t)
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProductionMode(true)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	cmd := NewProductionCmd()
	cmd.SetArgs([]string{"off", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("production off --force: %v", err)
	}
	reloaded, _ := config.LoadGlobal()
	if reloaded.ProductionMode() {
		t.Error("--force did not turn production mode off")
	}
}

// Off is a no-op when it is already off, and must not demand --force to do
// nothing. Refusing there teaches people to reach for the flag reflexively,
// which is exactly what it exists to prevent.
func TestProductionOff_AlreadyOffNeedsNoForce(t *testing.T) {
	productionEnv(t)
	cmd := NewProductionCmd()
	cmd.SetArgs([]string{"off"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("turning off an already-off mode asked for --force: %v", err)
	}
}

func TestProductionOn_RecordsTheMode(t *testing.T) {
	productionEnv(t)
	cmd := NewProductionCmd()
	cmd.SetArgs([]string{"on", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("production on: %v", err)
	}
	cfg, _ := config.LoadGlobal()
	if !cfg.ProductionMode() {
		t.Error("production mode was not recorded")
	}
	if cfg.ProductionSince().IsZero() {
		t.Error("no enabled-at time was recorded")
	}
}

// Both directions are audited. Which mode a machine was in is the first thing
// worth knowing when working out why it behaved as it did.
func TestProductionToggle_IsAudited(t *testing.T) {
	productionEnv(t)
	cmd := NewProductionCmd()
	cmd.SetArgs([]string{"on", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	entries := recentAuditActions(t)
	if !strings.Contains(entries, "production.enabled") {
		t.Errorf("turning production mode on was not audited: %s", entries)
	}
}

func recentAuditActions(t *testing.T) string {
	t.Helper()
	entries, err := auditlog.Recent(20)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Action + "\n")
	}
	return b.String()
}
