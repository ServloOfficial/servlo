package authz

import (
	"strings"
	"testing"
)

// Recovery codes exist for the phone that fell in a river. Without them,
// enabling TOTP is a way to lock yourself out of your own server, and the only
// way back is a shell on it.
func TestRecoveryCodes_AreGeneratedInABatch(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Errorf("got %d codes, want %d", len(codes), recoveryCodeCount)
	}
	if len(hashes) != len(codes) {
		t.Fatalf("got %d hashes for %d codes", len(hashes), len(codes))
	}

	seen := map[string]bool{}
	for _, code := range codes {
		if seen[code] {
			t.Errorf("code %q was issued twice", code)
		}
		seen[code] = true
		if len(code) < 10 {
			t.Errorf("code %q is short enough to be worth guessing", code)
		}
	}
}

// The stored form is a hash. A recovery code is a password that bypasses the
// second factor, so a leaked account file must not be a set of working ones.
func TestRecoveryCodes_AreStoredHashed(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	for i, hash := range hashes {
		if strings.Contains(hash, codes[i]) {
			t.Errorf("the stored hash contains the code itself: %q", hash)
		}
	}
}

func TestRecoveryCodes_ConsumeAcceptsOneCodeOnce(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}

	remaining, ok := ConsumeRecoveryCode(hashes, codes[0])
	if !ok {
		t.Fatal("a freshly issued recovery code was refused")
	}
	if len(remaining) != len(hashes)-1 {
		t.Errorf("%d codes remain, want %d", len(remaining), len(hashes)-1)
	}
	// One-time is the whole point: a code that keeps working is a password
	// written on a piece of paper.
	if _, ok := ConsumeRecoveryCode(remaining, codes[0]); ok {
		t.Error("a recovery code worked a second time")
	}
	// And the rest still work.
	if _, ok := ConsumeRecoveryCode(remaining, codes[1]); !ok {
		t.Error("consuming one code invalidated another")
	}
}

func TestRecoveryCodes_RejectsAnythingElse(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	for _, attempt := range []string{"", "not-a-code", codes[0] + "x", strings.ToUpper(codes[0]) + "z"} {
		if _, ok := ConsumeRecoveryCode(hashes, attempt); ok {
			t.Errorf("%q was accepted as a recovery code", attempt)
		}
	}
	// No codes left is not a state in which everything works.
	if _, ok := ConsumeRecoveryCode(nil, codes[0]); ok {
		t.Error("a code was accepted against an empty set")
	}
}

// Codes are read off a screen and typed back later, so the comparison is
// forgiving about the things a human changes: case, and the separator.
func TestRecoveryCodes_AreForgivingAboutHowTheyAreTyped(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	for _, typed := range []string{
		strings.ToUpper(codes[0]),
		" " + codes[0] + " ",
		strings.ReplaceAll(codes[0], "-", ""),
	} {
		if _, ok := ConsumeRecoveryCode(hashes, typed); !ok {
			t.Errorf("%q was refused, though it is the same code", typed)
		}
	}
}

// A code that is easy to read back is a code people will actually write down.
func TestRecoveryCodes_AreReadable(t *testing.T) {
	codes, _, err := NewRecoveryCodes()
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	for _, code := range codes {
		if !strings.Contains(code, "-") {
			t.Errorf("code %q has no separator, so it reads as one long run", code)
		}
		// Characters that are the same shape in most fonts get left out, or a
		// code written down is a code mistyped.
		if strings.ContainsAny(code, "01lIO") {
			t.Errorf("code %q contains a character that is easy to misread", code)
		}
	}
}
