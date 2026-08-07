package authz

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
)

// CSRF, and what it adds on top of SameSite=Strict.
//
// The session cookie is SameSite=Strict, which already means a browser will not
// attach it to a request another origin started. That is most of the defence.
// This is the rest of it: a token bound to the session, required on every
// state-changing route, so a request that somehow arrives with the cookie
// attached still has to prove it came from a page servlo rendered.
//
// It is per session rather than global. A site-wide token is one an attacker
// fetches for themselves and then replays against everyone.
//
// The key lives in this process and nowhere else. A restart invalidates
// outstanding tokens, which costs the operator a page reload and costs anything
// replaying one everything. Persisting it would buy the smoother reload and
// hand a stolen config file a working forgery kit.

var (
	csrfKeyOnce sync.Once
	csrfKey     []byte
)

func csrfSecret() []byte {
	csrfKeyOnce.Do(func() {
		csrfKey = make([]byte, 32)
		if _, err := rand.Read(csrfKey); err != nil {
			// Unreachable short of the system entropy source failing, at which
			// point session tokens are not being minted either. Leaving the key
			// zero would make every token forgeable, so panic rather than serve.
			panic("authz: no entropy for the CSRF key: " + err.Error())
		}
	})
	return csrfKey
}

// CSRFToken returns the token for a session. The same session always gets the
// same token within one process, so a page rendered once can post more than
// once without refetching it.
func CSRFToken(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, csrfSecret())
	mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyCSRF reports whether token was issued for sessionID.
func VerifyCSRF(sessionID, token string) bool {
	if sessionID == "" || token == "" {
		return false
	}
	want := CSRFToken(sessionID)
	return subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
}
