package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
)

func TestWorkerRows_SynthesizesFromSites(t *testing.T) {
	sites := []siteinfo.EnrichedSite{
		{
			Name: "alpha", Path: "/sites/alpha",
			HasQueueWorker: true, QueueRunning: true,
			HasHorizon: true, HorizonRunning: false,
		},
		{
			Name: "beta", Path: "/sites/beta",
			HasScheduleWorker: true, ScheduleRunning: true,
			FrameworkWorkers: []siteinfo.WorkerInfo{
				{Name: "broadcaster", Running: true},
			},
		},
	}
	rows := workerRows(sites)

	want := map[string]ServiceState{
		"queue-alpha":      stateRunning,
		"horizon-alpha":    stateStopped,
		"schedule-beta":    stateRunning,
		"broadcaster-beta": stateRunning,
	}
	if len(rows) != len(want) {
		t.Fatalf("expected %d worker rows, got %d: %+v", len(want), len(rows), rowsNames(rows))
	}
	for _, row := range rows {
		wantState, ok := want[row.Name]
		if !ok {
			t.Errorf("unexpected worker row %q", row.Name)
			continue
		}
		if row.State != wantState {
			t.Errorf("%s: state %v, want %v", row.Name, row.State, wantState)
		}
		if row.WorkerSite == "" || row.WorkerKind == "" || row.WorkerPath == "" {
			t.Errorf("%s: worker fields missing: %+v", row.Name, row)
		}
	}
}

func TestWorkerRows_MarksIdleSuspended(t *testing.T) {
	sites := []siteinfo.EnrichedSite{{
		Name: "alpha", Path: "/sites/alpha",
		HasQueueWorker:       true,
		QueueRunning:         false,
		IdleSuspendedWorkers: []string{"queue", "vite"},
		FrameworkWorkers:     []siteinfo.WorkerInfo{{Name: "vite"}},
	}}
	want := map[string]ServiceState{
		"queue-alpha": stateSuspended,
		"vite-alpha":  stateSuspended,
	}
	for _, row := range workerRows(sites) {
		if got := want[row.Name]; row.State != got {
			t.Errorf("%s: state %v, want %v (suspended)", row.Name, row.State, got)
		}
	}
}

func TestRenderServiceRow_OmitsSiteCount(t *testing.T) {
	// The site count moved to the service detail pane, so neither worker nor
	// plain service rows carry it in the list anymore.
	worker := stripANSI(renderServiceRow(false, ServiceRow{Name: "queue-acme", WorkerKind: "queue", SiteCount: 1, State: stateRunning}, 80))
	if strings.Contains(worker, "site") {
		t.Errorf("worker row should not show a site count, got %q", worker)
	}
	svc := stripANSI(renderServiceRow(false, ServiceRow{Name: "mysql", SiteCount: 3, State: stateRunning, Version: "8.0"}, 80))
	if strings.Contains(svc, "site") {
		t.Errorf("service row should no longer show a site count, got %q", svc)
	}
	if !strings.Contains(svc, "8.0") {
		t.Errorf("service row should still show its version, got %q", svc)
	}
}

func TestWorkerRows_FrameworkWorkerSkipsBuiltinNames(t *testing.T) {
	// When a framework redeclares queue / schedule / horizon / reverb in
	// FrameworkWorkers it's covered by the well-known branches already;
	// don't synthesise a duplicate row.
	sites := []siteinfo.EnrichedSite{{
		Name: "alpha", Path: "/x",
		FrameworkWorkers: []siteinfo.WorkerInfo{
			{Name: "queue", Running: true},
			{Name: "custom", Running: true},
		},
	}}
	rows := workerRows(sites)
	for _, r := range rows {
		if r.Name == "queue-alpha" {
			t.Errorf("framework-declared queue should be skipped, well-known branch handles it")
		}
	}
	if !hasName(rows, "custom-alpha") {
		t.Errorf("custom framework worker missing: %+v", rowsNames(rows))
	}
}

func rowsNames(rows []ServiceRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func hasName(rows []ServiceRow, name string) bool {
	for _, r := range rows {
		if r.Name == name {
			return true
		}
	}
	return false
}
