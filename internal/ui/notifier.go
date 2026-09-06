package ui

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/push"
)

// dispatchNotification is the single choke point for emitting notifications.
// Drops everything when the global notifier toggle is off (servlo notify off);
// otherwise the notification goes to the browser over the WebSocket, and to
// Web Push when no page is there to receive it.
//
// There is no desktop sink. A server panel has no desktop to post to, so the
// only place a notification can usefully arrive is a browser.
func dispatchNotification(n push.Notification) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return
	}
	if cfg == nil || !cfg.IsNotificationsEnabled() {
		return
	}
	payload, err := n.Payload()
	if err != nil {
		return
	}
	broker.broadcastNotification(payload)
	// A dashboard window with focus already received the frame above. Web Push
	// exists to reach a page that isn't there to listen. The test notification
	// is the exception: it is fired from the settings panel, which has focus,
	// and its whole job is to prove the delivery path works.
	if uiWindowFocused() && n.Kind != "test" {
		return
	}
	go func() {
		if err := push.Send(n); err != nil {
			fmt.Printf("[notifier] push send failed: %v\n", err)
		}
	}()
}
