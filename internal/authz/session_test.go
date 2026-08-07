package authz

import (
	"os"
	"strings"
	"testing"
	"time"
)

func sessionTestStore(t *testing.T) *SessionStore {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := OpenSessions()
	if err != nil {
		t.Fatalf("OpenSessions: %v", err)
	}
	return store
}

func TestSession_IssuedTokenAuthenticates(t *testing.T) {
	store := sessionTestStore(t)

	token, err := store.Create("alice", SessionMeta{IP: "203.0.113.9", UserAgent: "Firefox"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	session, ok := store.Lookup(token)
	if !ok {
		t.Fatal("a token issued a moment ago does not authenticate")
	}
	if session.User != "alice" {
		t.Errorf("session user = %q, want alice", session.User)
	}
	if session.IP != "203.0.113.9" {
		t.Errorf("session IP = %q", session.IP)
	}
	// What Lookup hands back goes into a request context and from there into
	// log lines and API responses, so it carries no part of the credential.
	if session.TokenHash != "" {
		t.Error("Lookup returned the token hash")
	}
}

// The store is a file on disk. If it held the tokens themselves, reading it
// would be the same as holding every live session, so it holds their hashes and
// the token exists only in the cookie.
func TestSession_StoreNeverHoldsTheTokenItself(t *testing.T) {
	store := sessionTestStore(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(SessionsPath())
	if err != nil {
		t.Fatalf("reading the session store: %v", err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("the session store contains the token, so reading the file is holding the session")
	}
}

// The file is 0600 at creation rather than chmodded after, so it is never
// briefly readable by another process on the machine.
func TestSession_StoreIsOwnerOnly(t *testing.T) {
	store := sessionTestStore(t)
	if _, err := store.Create("alice", SessionMeta{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, err := os.Stat(SessionsPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("session store mode = %04o, want 0600", mode)
	}
}

func TestSession_UnknownTokenIsRejected(t *testing.T) {
	store := sessionTestStore(t)
	if _, err := store.Create("alice", SessionMeta{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, token := range []string{"", "not-a-token", strings.Repeat("a", 43)} {
		if _, ok := store.Lookup(token); ok {
			t.Errorf("token %q authenticated", token)
		}
	}
}

// Signing out has to actually end the session server-side. A cookie the client
// throws away is not a sign-out: an attacker who copied it still has it.
func TestSession_RevokeEndsIt(t *testing.T) {
	store := sessionTestStore(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	session, _ := store.Lookup(token)
	if err := store.Revoke(session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := store.Lookup(token); ok {
		t.Fatal("a revoked session still authenticates")
	}
}

// Listing is what makes revoking usable: an operator has to be able to see the
// session before deciding it is not theirs.
func TestSession_ListShowsWhatIsSignedIn(t *testing.T) {
	store := sessionTestStore(t)

	if _, err := store.Create("alice", SessionMeta{IP: "203.0.113.9", UserAgent: "Firefox"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create("bob", SessionMeta{IP: "198.51.100.4", UserAgent: "Safari"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sessions := store.List()
	if len(sessions) != 2 {
		t.Fatalf("List returned %d sessions, want 2", len(sessions))
	}
	for _, s := range sessions {
		if s.ID == "" || s.User == "" || s.IP == "" || s.Created.IsZero() {
			t.Errorf("session is missing the fields an operator would decide on: %+v", s)
		}
		// The listing is shown in a terminal and a browser. A token in it
		// would be a token in a scrollback buffer.
		if s.TokenHash != "" {
			t.Error("List exposed the token hash")
		}
	}
}

func TestSession_RevokeAllForUser(t *testing.T) {
	store := sessionTestStore(t)

	aliceOne, _ := store.Create("alice", SessionMeta{})
	aliceTwo, _ := store.Create("alice", SessionMeta{})
	bob, _ := store.Create("bob", SessionMeta{})

	if err := store.RevokeUser("alice"); err != nil {
		t.Fatalf("RevokeUser: %v", err)
	}
	for _, token := range []string{aliceOne, aliceTwo} {
		if _, ok := store.Lookup(token); ok {
			t.Error("an alice session survived RevokeUser")
		}
	}
	if _, ok := store.Lookup(bob); !ok {
		t.Error("revoking alice's sessions took bob's with them")
	}
}

// An expired session is gone whether or not anything swept it, because the
// alternative is a cookie that outlives its own expiry by however long the
// sweep is late.
func TestSession_ExpiredSessionIsRejectedAndSwept(t *testing.T) {
	store := sessionTestStore(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.now = func() time.Time { return time.Now().Add(SessionTTL + time.Minute) }

	if _, ok := store.Lookup(token); ok {
		t.Fatal("an expired session still authenticates")
	}
	if len(store.List()) != 0 {
		t.Error("an expired session is still listed")
	}
}

// Using a session pushes its expiry out, so someone working all day is not
// signed out mid-deploy, while a session nobody has touched still ages out.
func TestSession_UseExtendsTheExpiry(t *testing.T) {
	store := sessionTestStore(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, _ := store.Lookup(token)

	store.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	second, ok := store.Lookup(token)
	if !ok {
		t.Fatal("the session expired while in use")
	}
	if !second.Expires.After(first.Expires) {
		t.Error("using a session did not push its expiry out")
	}
	if !second.LastSeen.After(first.LastSeen) {
		t.Error("using a session did not update when it was last seen")
	}
}

// Two processes hold the store: the panel and the CLI. A session created by one
// has to be visible to the other, which means reading through to the file
// rather than trusting an in-memory copy.
func TestSession_IsVisibleToAnotherProcess(t *testing.T) {
	store := sessionTestStore(t)

	token, err := store.Create("alice", SessionMeta{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	other, err := OpenSessions()
	if err != nil {
		t.Fatalf("OpenSessions: %v", err)
	}
	if _, ok := other.Lookup(token); !ok {
		t.Fatal("a session created by one process is invisible to another")
	}
	session, _ := other.Lookup(token)
	if err := other.Revoke(session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := store.Lookup(token); ok {
		t.Fatal("a session revoked from the CLI still authenticates in the panel")
	}
}

// Tokens have to be unguessable, and the check that matters is that they are
// long and never repeat.
func TestSession_TokensAreLongAndUnique(t *testing.T) {
	store := sessionTestStore(t)

	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		token, err := store.Create("alice", SessionMeta{})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if len(token) < 32 {
			t.Fatalf("token %q is %d characters, too short to resist guessing", token, len(token))
		}
		if seen[token] {
			t.Fatal("two sessions were issued the same token")
		}
		seen[token] = true
	}
}
