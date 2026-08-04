package cli

import (
	"reflect"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// workerMigrationStep describes one per-worker action the migration will
// take when the user flips servlo workers mode. Keeping the plan as pure
// data lets us unit-test the decision layer without touching podman,
// launchd, or the disk.

func TestWorkerMigrationPlan_NoOpWhenModeUnchanged(t *testing.T) {
	plan := planWorkerMigration(config.WorkerExecModeExec, config.WorkerExecModeExec, []string{"servlo-queue-alpha", "servlo-horizon-beta"})
	if len(plan) != 0 {
		t.Errorf("unchanged mode should produce empty plan, got %+v", plan)
	}
}

func TestWorkerMigrationPlan_ContainerToExec(t *testing.T) {
	got := planWorkerMigration(
		config.WorkerExecModeContainer,
		config.WorkerExecModeExec,
		[]string{"servlo-queue-alpha", "servlo-horizon-beta"},
	)
	want := []workerMigrationStep{
		{
			Unit:    "servlo-queue-alpha",
			From:    config.WorkerExecModeContainer,
			To:      config.WorkerExecModeExec,
			OldKind: artifactContainer,
			NewKind: artifactService,
		},
		{
			Unit:    "servlo-horizon-beta",
			From:    config.WorkerExecModeContainer,
			To:      config.WorkerExecModeExec,
			OldKind: artifactContainer,
			NewKind: artifactService,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plan mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestWorkerMigrationPlan_ExecToContainer(t *testing.T) {
	got := planWorkerMigration(
		config.WorkerExecModeExec,
		config.WorkerExecModeContainer,
		[]string{"servlo-queue-alpha"},
	)
	want := []workerMigrationStep{
		{
			Unit:    "servlo-queue-alpha",
			From:    config.WorkerExecModeExec,
			To:      config.WorkerExecModeContainer,
			OldKind: artifactService,
			NewKind: artifactContainer,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plan mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestWorkerMigrationPlan_EmptyUnitList(t *testing.T) {
	plan := planWorkerMigration(config.WorkerExecModeContainer, config.WorkerExecModeExec, nil)
	if len(plan) != 0 {
		t.Errorf("no units → empty plan, got %+v", plan)
	}
}
