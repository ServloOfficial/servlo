package authz

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// credentialShaped reports whether a field's name says it carries a credential.
//
// A heuristic on the name, and worth being honest about what that does and does
// not buy. It catches the case that actually happens, a field added later whose
// name says plainly what it holds, and it cannot catch a secret called
// something else. It is a floor under the reasoning, not a proof.
func credentialShaped(name string) bool {
	lower := strings.ToLower(name)
	for _, mark := range []string{"hash", "secret", "token", "password", "passphrase", "key", "pepper", "salt"} {
		if strings.Contains(lower, mark) {
			return true
		}
	}
	return false
}

// redact is the one place an account is stripped before it leaves this package,
// and its comment used to say that forgetting a field would be a compile error
// at the struct. Go offers no such thing: a field added to Account and not
// stripped here compiles, passes every test, and ships the credential to the
// panel. The TOTP secret is the one that makes it matter, since it is a second
// factor anyone holding it can generate forever.
//
// So this is the check that comment described. Every field is given a non-zero
// value, the account is redacted, and any field whose name says it holds a
// credential has to come back empty.
func TestRedact_StripsEveryCredentialShapedFieldOnAccount(t *testing.T) {
	full := Account{
		Name:            "alice",
		PasswordHash:    "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		Role:            RoleAdmin,
		Created:         time.Now(),
		Sites:           []string{"shop.example"},
		TOTPSecret:      "JBSWY3DPEHPK3PXP",
		TOTPEnabled:     true,
		TOTPLastCounter: 57012345,
		RecoveryHashes:  []string{"one", "two", "three"},
		RecoveryLeft:    3,
	}

	// Every field has to be set, or a field this test forgot would look
	// stripped because it was never populated.
	v := reflect.ValueOf(full)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Fatalf("this test does not set Account.%s, so it cannot tell whether redact strips it",
				v.Type().Field(i).Name)
		}
	}

	got := reflect.ValueOf(redact(full))
	for i := 0; i < got.NumField(); i++ {
		name := got.Type().Field(i).Name
		if !credentialShaped(name) {
			continue
		}
		if !got.Field(i).IsZero() {
			t.Errorf("redact leaves Account.%s set, so it reaches whatever the panel is handed", name)
		}
	}
}

// The other half: redact must not strip what the panel needs, or an operator
// cannot see who their accounts are.
func TestRedact_KeepsWhatThePanelHasToShow(t *testing.T) {
	got := redact(Account{
		Name:           "alice",
		Role:           RoleDeveloper,
		Sites:          []string{"shop.example"},
		TOTPEnabled:    true,
		RecoveryHashes: []string{"one", "two"},
		PasswordHash:   "hash",
	})

	if got.Name != "alice" || got.Role != RoleDeveloper || len(got.Sites) != 1 {
		t.Errorf("redact took away the identity the panel lists: %+v", got)
	}
	if !got.TOTPEnabled {
		t.Error("redact hid that the account has a second factor, which the panel has to show")
	}
	// The count survives even though the hashes do not: the panel shows how
	// many recovery codes are left so the operator knows when to make more.
	if got.RecoveryLeft != 2 {
		t.Errorf("RecoveryLeft = %d, want 2", got.RecoveryLeft)
	}
}
