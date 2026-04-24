package serviceops

import (
	"testing"
)

func TestInstallPresetStreaming_UnknownPresetEmitsConfigPhaseAndErrors(t *testing.T) {
	var events []PhaseEvent
	svc, err := InstallPresetStreaming("definitely-not-a-real-preset-xyz", "", func(e PhaseEvent) {
		events = append(events, e)
	})
	if err == nil {
		t.Fatalf("expected error for unknown preset, got svc=%+v", svc)
	}
	if svc != nil {
		t.Fatalf("expected nil service on error, got %+v", svc)
	}
	if len(events) != 1 || events[0].Phase != "installing_config" {
		t.Fatalf("expected single installing_config event before the error, got %+v", events)
	}
}
