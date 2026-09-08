package authz

import (
	"testing"
	"time"
)

// A session that has been revoked is not live, and one that has expired is not
// live either.
//
// This is what a long-lived connection has to be able to ask. Every route looks
// the session up on the way past, so a revoked operator is refused the moment
// they click anything. The websocket is the one surface with no next request:
// it is authorised once at the handshake and then streams the panel's live
// state until the browser goes away, so without a question it can ask on a
// timer, `servlo sessions revoke` ends the session everywhere except the place
// the operator is actually watching.
func TestSessionStore_LiveFollowsRevocation(t *testing.T) {
	store := newTestSessions(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	session, ok := store.Lookup(token)
	if !ok {
		t.Fatal("the session that was just created does not look up")
	}

	if !store.Live(session.ID) {
		t.Fatal("a session that exists reports as not live")
	}
	if err := store.Revoke(session.ID); err != nil {
		t.Fatal(err)
	}
	if store.Live(session.ID) {
		t.Error("a revoked session still reports as live, so a connection holding it keeps streaming")
	}
}

func TestSessionStore_LiveIsFalseOnceExpired(t *testing.T) {
	store := newTestSessions(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	session, _ := store.Lookup(token)

	store.now = func() time.Time { return time.Now().Add(SessionTTL + time.Hour) }
	if store.Live(session.ID) {
		t.Error("an expired session reports as live")
	}
}

// Live must not renew. Lookup extends a session on every request because a
// request is somebody using the panel; a liveness check on a timer is not, and
// renewing from one would keep a session alive forever behind a tab nobody is
// looking at.
func TestSessionStore_LiveDoesNotExtendTheSession(t *testing.T) {
	store := newTestSessions(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	session, _ := store.Lookup(token)
	before := session.Expires

	store.now = func() time.Time { return time.Now().Add(time.Hour) }
	if !store.Live(session.ID) {
		t.Fatal("a session an hour old is not live")
	}

	store.now = time.Now
	after, ok := store.Lookup(token)
	if !ok {
		t.Fatal("the session went away")
	}
	if after.Expires.After(before.Add(time.Minute)) {
		t.Errorf("Live extended the session: expiry moved from %s to %s", before, after.Expires)
	}
}

func newTestSessions(t *testing.T) *SessionStore {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := OpenSessions()
	if err != nil {
		t.Fatal(err)
	}
	return store
}
