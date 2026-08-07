package authz

import (
	"strings"
	"testing"
)

// CSRF is bound to the session, not global. A single site-wide token would be
// one an attacker can fetch for themselves and then replay against anyone.
func TestCSRF_TokenVerifiesAgainstItsOwnSession(t *testing.T) {
	token := CSRFToken("session-one")
	if token == "" {
		t.Fatal("CSRFToken returned nothing")
	}
	if !VerifyCSRF("session-one", token) {
		t.Error("a token did not verify against the session it was made for")
	}
	if VerifyCSRF("session-two", token) {
		t.Error("a token made for one session verified against another")
	}
}

func TestCSRF_RejectsAnythingElse(t *testing.T) {
	valid := CSRFToken("session-one")
	for _, token := range []string{
		"",
		"not-a-token",
		valid + "x",
		strings.TrimSuffix(valid, valid[len(valid)-1:]),
		strings.ToUpper(valid),
	} {
		if VerifyCSRF("session-one", token) {
			t.Errorf("token %q verified", token)
		}
	}
	// An empty session id must not become a skeleton key: a request with no
	// session should be failing the session check, not passing CSRF.
	if VerifyCSRF("", "") || VerifyCSRF("", valid) {
		t.Error("an empty session id verified a token")
	}
}

// The token is derived from a key that lives only in this process, so a
// restart invalidates outstanding tokens. That is a page reload for the
// operator and a dead end for anything replaying one.
func TestCSRF_TokenIsStableWithinAProcess(t *testing.T) {
	first := CSRFToken("session-one")
	second := CSRFToken("session-one")
	if first != second {
		t.Error("the same session got two different tokens, so a page rendered once cannot post twice")
	}
}

// The token goes in a response header and a form field. It has to be safe to
// put in both without escaping.
func TestCSRF_TokenIsHeaderSafe(t *testing.T) {
	token := CSRFToken("session-one")
	if strings.ContainsAny(token, " \t\r\n\"';,=") {
		t.Errorf("token %q contains a character that needs escaping in a header", token)
	}
	if len(token) < 32 {
		t.Errorf("token %q is %d characters, short enough to be worth guessing", token, len(token))
	}
}
