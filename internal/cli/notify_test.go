package cli

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestRunNotifyToggle_DisableThenEnable(t *testing.T) {
	withTempXDG(t)

	if err := runNotifyToggle(false); err != nil {
		t.Fatalf("runNotifyToggle off: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsNotificationsEnabled() {
		t.Error("notifications still enabled after off")
	}

	if err := runNotifyToggle(true); err != nil {
		t.Fatalf("runNotifyToggle on: %v", err)
	}
	cfg, _ = config.LoadGlobal()
	if !cfg.IsNotificationsEnabled() {
		t.Error("notifications not re-enabled after on")
	}
}

func TestRunNotifyToggle_NoChangeOnSecondCall(t *testing.T) {
	withTempXDG(t)
	if err := runNotifyToggle(false); err != nil {
		t.Fatal(err)
	}
	if err := runNotifyToggle(false); err != nil {
		t.Fatalf("second off call: %v", err)
	}
	cfg, _ := config.LoadGlobal()
	if cfg.IsNotificationsEnabled() {
		t.Error("expected notifications still disabled")
	}
}

func TestNewNotifyCmd_HasExpectedSubcommands(t *testing.T) {
	cmd := NewNotifyCmd()
	want := []string{"on", "off", "status"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	var missing []string
	for _, w := range want {
		if !have[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing subcommand(s) %s", strings.Join(missing, ", "))
	}
}
