package ui

import (
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/push"
)

// notifyGateConfigDir points config to a fresh per-test directory.
func notifyGateConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
}

func TestDispatchNotification_BroadcastsWhenEnabled(t *testing.T) {
	notifyGateConfigDir(t)
	ch := broker.add()
	defer broker.remove(ch)

	dispatchNotification(push.Notification{Kind: "test", Title: "t", Body: "b"})

	select {
	case msg := <-ch:
		if len(msg.Notification) == 0 {
			t.Error("expected non-empty notification payload")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected broker to receive notification when notifications are enabled")
	}
}

func TestDispatchNotification_SkipsBroadcastWhenDisabled(t *testing.T) {
	notifyGateConfigDir(t)

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.SetNotificationsEnabled(false)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	ch := broker.add()
	defer broker.remove(ch)

	dispatchNotification(push.Notification{Kind: "test", Title: "t", Body: "b"})

	select {
	case msg := <-ch:
		t.Fatalf("expected no broadcast when notifications disabled, got %d bytes", len(msg.Notification))
	case <-time.After(100 * time.Millisecond):
	}
}
