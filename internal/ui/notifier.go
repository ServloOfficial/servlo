package ui

import (
	"fmt"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/push"
)

// dispatchNotification is the single choke point for emitting notifications.
// Drops everything when the global notifier toggle is off (lerd notify off /
// tray). Per-device prefs still apply downstream when the gate is open.
func dispatchNotification(n push.Notification) {
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil && !cfg.IsNotificationsEnabled() {
		return
	}
	payload, err := n.Payload()
	if err != nil {
		return
	}
	broker.broadcastNotification(payload)
	go func() {
		if err := push.Send(n); err != nil {
			fmt.Printf("[notifier] push send failed: %v\n", err)
		}
	}()
}
