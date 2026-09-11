package cli

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/dbconn"
)

// A managed database is reached over the public internet, and TLS off is the
// zero value of the field that protects it. So omitting --tls did not mean "I
// thought about it", it meant nothing was typed, and the connection carried the
// administrative password and every query in the clear.
//
// The help for the flag offers require or verify-ca and never offered none, the
// database page says verify-ca "is what you want", and the worked example in it
// passes the flag. Every word around this said a managed connection is
// protected. Only the default said otherwise.
//
// So the flag has to be answered rather than defaulted. None is spellable, for
// a provider on a private network where it is a real choice, and it has to be
// spelled.
func TestManagedTLSMode_MustBeAnswered(t *testing.T) {
	if _, err := managedTLSMode(""); err == nil {
		t.Error("a managed connection with no TLS mode was accepted, so omitting the flag still sends the password in the clear")
	} else {
		for _, want := range []string{"verify-ca", "require", "none"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not name %q, so it does not say what to type: %v", want, err)
			}
		}
	}
}

// The three answers, and what each stores. "none" is the word an operator types
// and the empty string is what the rest of servlo already reads for it, so the
// spelling is translated here rather than changing what is on disk.
func TestManagedTLSMode_TranslatesTheAnswers(t *testing.T) {
	for _, tc := range []struct{ typed, stored string }{
		{"none", dbconn.TLSOff},
		{"require", dbconn.TLSRequire},
		{"verify-ca", dbconn.TLSVerifyCA},
		{"  require  ", dbconn.TLSRequire},
		{"VERIFY-CA", dbconn.TLSVerifyCA},
	} {
		got, err := managedTLSMode(tc.typed)
		if err != nil {
			t.Errorf("managedTLSMode(%q): %v", tc.typed, err)
			continue
		}
		if got != tc.stored {
			t.Errorf("managedTLSMode(%q) = %q, want %q", tc.typed, got, tc.stored)
		}
	}
}

// Anything else is a typo, and a typo that fell through to off would be the
// same leak by another route.
func TestManagedTLSMode_RefusesAnythingElse(t *testing.T) {
	for _, bad := range []string{"yes", "ssl", "verify_ca", "true", "off"} {
		if _, err := managedTLSMode(bad); err == nil {
			t.Errorf("managedTLSMode(%q) was accepted", bad)
		}
	}
}
