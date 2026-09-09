package ui

import (
	"testing"
	"time"
)

// chooseInterval is the only branching point that decides the cache poll
// cadence, and an open dashboard tab is the only thing it may look at. The
// panel runs on the server; whether anyone is watching it is a fact about a
// browser on somebody else's machine, and no property of this machine's own
// login sessions stands in for it.
func TestChooseInterval(t *testing.T) {
	cases := []struct {
		name    string
		visible int32
		want    time.Duration
	}{
		{"one tab open", 1, intervalFocused},
		{"several tabs open", 7, intervalFocused},
		{"no tabs open", 0, intervalIdle},
		{"negative counter", -1, intervalIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseInterval(tc.visible); got != tc.want {
				t.Errorf("chooseInterval(%d) = %v, want %v", tc.visible, got, tc.want)
			}
		})
	}
}

// intervalFocused must be strictly shorter than intervalIdle, otherwise the
// "fast cadence on focused tabs" promise is meaningless. Pin the invariant
// so a future tweak to the constants can't silently break it.
func TestFocusedIntervalIsFasterThanIdle(t *testing.T) {
	if intervalFocused >= intervalIdle {
		t.Errorf("intervalFocused (%v) must be < intervalIdle (%v)", intervalFocused, intervalIdle)
	}
}
