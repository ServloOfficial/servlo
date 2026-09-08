package ui

import "testing"

// The websocket is the one surface that authorises once and then keeps going.
//
// Every route looks the session up on the way past, so a revoked operator is
// refused the moment they click anything. A connection has no next click: it
// was authorised at the handshake and then streams the site list, the service
// state, worker health and every notification for as long as the browser holds
// it open. So `servlo sessions revoke`, whose whole purpose is the laptop that
// is no longer in the building, ended the session everywhere except the place
// that laptop was actually watching.
func TestWSSessionEnded(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		live  bool
		ended bool
	}{
		{"a live session keeps its connection", "sess-1", true, false},
		{"a revoked session loses it", "sess-1", false, true},
		{"a panel with no accounts has nothing to check", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			restore := swapSessionLive(func(string) bool { return tc.live })
			defer restore()

			if got := wsSessionEnded(tc.id); got != tc.ended {
				t.Errorf("wsSessionEnded(%q) = %v, want %v", tc.id, got, tc.ended)
			}
		})
	}
}

// A store that cannot be read is not evidence the session is gone. Dropping
// every open dashboard on a transient read error would be a worse failure than
// the one this guards against.
func TestWSSessionEnded_KeepsTheConnectionWhenTheStoreCannotBeRead(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/proc/servlo-does-not-exist")
	if wsSessionEnded("sess-1") {
		t.Error("an unreadable session store closed a connection")
	}
}

func swapSessionLive(fn func(string) bool) func() {
	old := sessionLive
	sessionLive = fn
	return func() { sessionLive = old }
}
