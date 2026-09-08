package authz

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Sessions, and why they are records rather than a signed cookie.
//
// Upstream signed the username and an expiry with the password hash as the key,
// which authenticates without storing anything. It also means a session cannot
// be ended: the only way to invalidate one is to change the password, which
// invalidates every session on every device at once. S5.2 asks for sessions
// listable and revocable from the CLI, and neither is possible without a record
// per session.
//
// The store holds the SHA-256 of each token, never the token. The token exists
// in the cookie and nowhere else, so reading the file is not the same as
// holding every live session. A hash is enough here without a slow KDF: a
// 256-bit random token has no smaller search space to grind.

const (
	// SessionTTL is how long a session survives without being used. A week
	// keeps someone signed in across a working stretch while a laptop left in
	// a hotel eventually stops being a way in.
	SessionTTL = 7 * 24 * time.Hour

	// sessionTokenBytes is 256 bits, which is not guessable and is short
	// enough to sit in a cookie without comment.
	sessionTokenBytes = 32
)

// Session is one signed-in browser.
type Session struct {
	ID        string    `json:"id"`
	User      string    `json:"user"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"last_seen"`
	Expires   time.Time `json:"expires"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	// TokenHash is how a presented token is matched to this record. It is
	// cleared on the copies List and Lookup hand out, because those go to a
	// terminal and a browser and a hash in a scrollback is a hash in a
	// scrollback.
	TokenHash string `json:"token_hash,omitempty"`
}

// SessionMeta is what a caller knows about a session at sign-in.
type SessionMeta struct {
	IP        string
	UserAgent string
}

// SessionStore is the on-disk set of sessions. Two processes hold it, the panel
// and the CLI, so every operation reads the file rather than trusting a copy in
// memory: a session revoked from a shell has to stop working in the panel
// immediately, not at the next restart.
type SessionStore struct {
	mu  sync.Mutex
	now func() time.Time
}

// SessionsPath is where the session records live.
func SessionsPath() string { return filepath.Join(config.DataDir(), "sessions.json") }

// OpenSessions returns a handle to the session store, creating its directory.
func OpenSessions() (*SessionStore, error) {
	if err := os.MkdirAll(filepath.Dir(SessionsPath()), 0700); err != nil {
		return nil, fmt.Errorf("creating the session directory: %w", err)
	}
	return &SessionStore{now: time.Now}, nil
}

// Create issues a session for user and returns its token, which is the only
// time the token exists outside the caller's cookie.
func (s *SessionStore) Create(user string, meta SessionMeta) (string, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating a session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.load()
	if err != nil {
		return "", err
	}
	now := s.now()
	sessions = append(sessions, Session{
		ID:        newSessionID(),
		User:      user,
		Created:   now,
		LastSeen:  now,
		Expires:   now.Add(SessionTTL),
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
		TokenHash: hashToken(token),
	})
	if err := s.save(sessions); err != nil {
		return "", err
	}
	return token, nil
}

// Lookup returns the session a token authenticates as, and extends it.
//
// Extending on use is what keeps someone working all day from being signed out
// mid-deploy, while a session nobody has touched still ages out.
func (s *SessionStore) Lookup(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.load()
	if err != nil {
		return Session{}, false
	}
	want := hashToken(token)
	now := s.now()
	for i := range sessions {
		if subtle.ConstantTimeCompare([]byte(sessions[i].TokenHash), []byte(want)) != 1 {
			continue
		}
		if now.After(sessions[i].Expires) {
			return Session{}, false
		}
		sessions[i].LastSeen = now
		sessions[i].Expires = now.Add(SessionTTL)
		found := sessions[i]
		// A write per request is a write per request. It is one small file and
		// the alternative is an expiry that only advances when something else
		// happens to save, which is worse to reason about than it is to pay for.
		_ = s.save(sessions)
		found.TokenHash = ""
		return found, true
	}
	return Session{}, false
}

// Live reports whether the session with this ID is still in the store and has
// not expired.
//
// For the one surface that authorises once and then keeps going. Every route
// looks a session up on the way past, so a revoked operator is refused at their
// next click; a websocket has no next click, and without this it would stream
// the panel's state until the browser went away.
//
// Read-only on purpose. Lookup extends a session on every request because a
// request is somebody using the panel. A liveness check on a timer is not, and
// renewing from one would keep a session alive indefinitely behind a tab nobody
// is looking at.
func (s *SessionStore) Live(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.load()
	if err != nil {
		// A store that cannot be read is not evidence the session is gone, and
		// closing every connection on a transient read error would turn a
		// blipping disk into a dashboard that will not stay up.
		return true
	}
	now := s.now()
	for i := range sessions {
		if sessions[i].ID == id {
			return !now.After(sessions[i].Expires)
		}
	}
	return false
}

// List returns the live sessions, newest first, without their token hashes.
func (s *SessionStore) List() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.load()
	if err != nil {
		return nil
	}
	out := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		session.TokenHash = ""
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// Revoke ends one session by its id.
func (s *SessionStore) Revoke(id string) error {
	return s.filter(func(session Session) bool { return session.ID != id })
}

// RevokeUser ends every session belonging to one user, which is what a
// password change and a lost device both need.
func (s *SessionStore) RevokeUser(user string) error {
	return s.filter(func(session Session) bool { return session.User != user })
}

// RevokeAll ends every session.
func (s *SessionStore) RevokeAll() error {
	return s.filter(func(Session) bool { return false })
}

func (s *SessionStore) filter(keep func(Session) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.load()
	if err != nil {
		return err
	}
	kept := sessions[:0]
	for _, session := range sessions {
		if keep(session) {
			kept = append(kept, session)
		}
	}
	return s.save(kept)
}

// load reads the store, dropping expired records on the way through so an
// expired session is gone whether or not anything swept it.
func (s *SessionStore) load() ([]Session, error) {
	data, err := os.ReadFile(SessionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the session store: %w", err)
	}
	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		// A store servlo cannot parse is a store with no valid sessions in it.
		// Treating it as empty signs everyone out, which is the safe direction:
		// the alternative is guessing at what a corrupt file meant.
		return nil, nil
	}
	now := s.now()
	live := sessions[:0]
	for _, session := range sessions {
		if now.Before(session.Expires) {
			live = append(live, session)
		}
	}
	return live, nil
}

// save writes the store 0600, through a temporary file in the same directory so
// a crash mid-write leaves the previous set rather than a truncated one.
func (s *SessionStore) save(sessions []Session) error {
	data, err := json.Marshal(sessions)
	if err != nil {
		return err
	}
	dir := filepath.Dir(SessionsPath())
	tmp, err := os.CreateTemp(dir, ".sessions-*.json")
	if err != nil {
		return fmt.Errorf("creating a temporary session store: %w", err)
	}
	defer os.Remove(tmp.Name())
	// Created 0600 rather than chmodded after, so it is never briefly readable.
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), SessionsPath())
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newSessionID is a short handle for the operator to name a session by. It is
// not a secret: knowing one lets you revoke a session, never use it.
func newSessionID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		// Only reached if the system entropy source fails, at which point the
		// token generation above has already failed too.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))[:16]
	}
	return hex.EncodeToString(raw)
}
