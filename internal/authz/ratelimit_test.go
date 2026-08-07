package authz

import (
	"testing"
	"time"
)

func testLimiter(t *testing.T) *Limiter {
	t.Helper()
	return NewLimiter()
}

// A panel on the internet is a login form anyone can reach, so the cost of a
// wrong guess has to rise. The first few are free, because an operator
// mistyping their own password is the common case and locking them out on the
// second attempt is its own kind of outage.
func TestLimiter_FirstAttemptsAreFree(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < freeAttempts; i++ {
		if wait := limiter.Retry("203.0.113.9"); wait != 0 {
			t.Fatalf("attempt %d asked for a wait of %v before any failure", i+1, wait)
		}
		limiter.Failed("203.0.113.9")
	}
}

// Progressive, not fixed: a fixed delay is a rate an attacker simply plans
// around, while a doubling one turns a dictionary run into something that
// outlives the attacker's patience.
func TestLimiter_LockoutGrowsWithEachFailure(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < freeAttempts; i++ {
		limiter.Failed("203.0.113.9")
	}

	var previous time.Duration
	for i := 0; i < 5; i++ {
		limiter.Failed("203.0.113.9")
		wait := limiter.Retry("203.0.113.9")
		if wait <= previous {
			t.Fatalf("failure %d gave a wait of %v, not longer than the previous %v", freeAttempts+i+1, wait, previous)
		}
		previous = wait
	}
}

// Unbounded doubling reaches a week, which is indistinguishable from having
// broken the panel. The ceiling is long enough to make guessing hopeless and
// short enough that an operator who locked themselves out can wait it out.
func TestLimiter_LockoutIsCapped(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < 40; i++ {
		limiter.Failed("203.0.113.9")
	}
	if wait := limiter.Retry("203.0.113.9"); wait > maxLockout {
		t.Errorf("wait = %v, above the %v ceiling", wait, maxLockout)
	}
}

// One address failing must not lock out another, or anyone can deny the
// operator their own panel by guessing badly from somewhere else.
func TestLimiter_IsPerAddress(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < 20; i++ {
		limiter.Failed("203.0.113.9")
	}
	if wait := limiter.Retry("198.51.100.4"); wait != 0 {
		t.Errorf("an unrelated address must wait %v because of someone else's failures", wait)
	}
}

// Signing in successfully clears the record, so yesterday's typo does not
// shorten today's patience.
func TestLimiter_SuccessClearsTheRecord(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < 10; i++ {
		limiter.Failed("203.0.113.9")
	}
	limiter.Succeeded("203.0.113.9")
	if wait := limiter.Retry("203.0.113.9"); wait != 0 {
		t.Errorf("a successful sign-in left a %v wait behind", wait)
	}
}

// The wait counts down. A lockout that never expired would need a CLI command
// to clear and would turn one bad afternoon into a support ticket.
func TestLimiter_WaitElapses(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < freeAttempts+3; i++ {
		limiter.Failed("203.0.113.9")
	}
	wait := limiter.Retry("203.0.113.9")
	if wait == 0 {
		t.Fatal("no lockout after repeated failures")
	}

	limiter.now = func() time.Time { return time.Now().Add(wait + time.Second) }
	if remaining := limiter.Retry("203.0.113.9"); remaining != 0 {
		t.Errorf("the lockout still asks for %v after it should have elapsed", remaining)
	}
}

// A record nobody has touched in a long time is forgotten, so the limiter is
// not a memory leak an attacker fills by rotating source addresses.
func TestLimiter_ForgetsIdleAddresses(t *testing.T) {
	limiter := testLimiter(t)
	for i := 0; i < 200; i++ {
		limiter.Failed(testAddress(i))
	}
	limiter.now = func() time.Time { return time.Now().Add(recordTTL + time.Minute) }
	limiter.Failed("203.0.113.1")

	if tracked := limiter.Tracked(); tracked > 1 {
		t.Errorf("the limiter still tracks %d addresses after they aged out", tracked)
	}
}

func testAddress(i int) string {
	return "203.0.113." + string(rune('0'+i%10)) + "." + string(rune('0'+i/10%10))
}
