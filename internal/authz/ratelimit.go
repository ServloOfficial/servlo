package authz

import (
	"sync"
	"time"
)

// Rate limiting the login, and why the delay grows.
//
// A panel on the internet is a login form anyone can reach. A fixed delay is a
// rate an attacker plans around: at one attempt a second a dictionary still
// finishes overnight. A doubling one turns the same run into something that
// outlives the attacker's patience, while costing an operator who mistyped
// their own password nothing until they have done it several times.
//
// It is per source address, because a global lockout is a denial of service
// anyone can trigger against the operator by guessing badly from anywhere.

const (
	// freeAttempts is how many failures cost nothing. An operator mistyping a
	// long passphrase twice is ordinary; locking them out for it is its own
	// kind of outage.
	freeAttempts = 3

	// baseLockout is the wait after the first failure past the free ones, and
	// it doubles from there.
	baseLockout = 2 * time.Second

	// maxLockout is the ceiling. Unbounded doubling reaches a week, which is
	// indistinguishable from having broken the panel; fifteen minutes makes
	// guessing hopeless and is short enough to wait out.
	maxLockout = 15 * time.Minute

	// recordTTL is how long an address is remembered after its last attempt,
	// so the limiter is not a memory leak an attacker fills by rotating source
	// addresses.
	recordTTL = time.Hour
)

type attemptRecord struct {
	failures int
	last     time.Time
}

// Limiter tracks failed sign-ins per source address.
//
// In memory rather than on disk, deliberately. A restart clears it, which
// sounds like a weakness and is not: restarting servlo needs access to the
// machine, and anyone with that has no reason to be guessing at the login.
// Persisting it would mean a disk write on every failed attempt, which is a
// denial of service an attacker can aim at the disk.
type Limiter struct {
	mu      sync.Mutex
	records map[string]*attemptRecord
	now     func() time.Time
}

// NewLimiter returns an empty limiter.
func NewLimiter() *Limiter {
	return &Limiter{records: map[string]*attemptRecord{}, now: time.Now}
}

// Retry returns how long the address must wait before its next attempt is
// worth making. Zero means now.
func (l *Limiter) Retry(addr string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	record, ok := l.records[addr]
	if !ok || record.failures <= freeAttempts {
		return 0
	}
	elapsed := l.now().Sub(record.last)
	wait := lockoutFor(record.failures)
	if elapsed >= wait {
		return 0
	}
	return wait - elapsed
}

// Failed records a failed attempt from addr.
func (l *Limiter) Failed(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep()
	record, ok := l.records[addr]
	if !ok {
		record = &attemptRecord{}
		l.records[addr] = record
	}
	record.failures++
	record.last = l.now()
}

// Succeeded clears the record for addr, so yesterday's typo does not shorten
// today's patience.
func (l *Limiter) Succeeded(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, addr)
}

// Tracked is how many addresses the limiter is remembering. Exposed so a test
// can assert the sweep actually sweeps.
func (l *Limiter) Tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records)
}

// sweep drops records nothing has touched in recordTTL. Called on write rather
// than on a timer: attempts are the only thing that grows the map, so they are
// the only moment it needs shrinking.
func (l *Limiter) sweep() {
	cutoff := l.now().Add(-recordTTL)
	for addr, record := range l.records {
		if record.last.Before(cutoff) {
			delete(l.records, addr)
		}
	}
}

// lockoutFor doubles from baseLockout for each failure past the free ones,
// stopping at the ceiling.
func lockoutFor(failures int) time.Duration {
	over := failures - freeAttempts
	if over <= 0 {
		return 0
	}
	wait := baseLockout
	for i := 1; i < over; i++ {
		wait *= 2
		if wait >= maxLockout {
			return maxLockout
		}
	}
	return wait
}
