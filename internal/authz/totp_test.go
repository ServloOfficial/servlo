package authz

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238 test vectors, which is the only way to know the implementation is
// TOTP rather than something that merely behaves like it. A code Google
// Authenticator will not produce is a code nobody can enter.
//
// The seed is the RFC's ASCII "12345678901234567890" in base32, SHA-1, 8
// digits, 30-second step.
func TestTOTP_MatchesTheRFCVectors(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		got := totpAt(secret, time.Unix(tc.unix, 0), 8)
		if got != tc.want {
			t.Errorf("totpAt(%d) = %q, want %q", tc.unix, got, tc.want)
		}
	}
}

// What servlo actually issues is six digits, which is what every authenticator
// app shows.
func TestTOTP_IssuesSixDigits(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}
	code := totpAt(secret, time.Now(), totpDigits)
	if len(code) != 6 {
		t.Errorf("code %q is %d digits, want 6", code, len(code))
	}
	if strings.Trim(code, "0123456789") != "" {
		t.Errorf("code %q is not all digits", code)
	}
}

func TestTOTP_VerifiesTheCurrentCode(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}
	now := time.Now()
	if !verifyTOTPAt(secret, totpAt(secret, now, totpDigits), now) {
		t.Error("the current code did not verify")
	}
	if verifyTOTPAt(secret, "000000", now) && totpAt(secret, now, totpDigits) != "000000" {
		t.Error("a wrong code verified")
	}
}

// One step of tolerance either way, and no more. A phone clock is rarely exact,
// and a code typed as the window turns over should still work; a wider window
// is a longer life for a code someone read over a shoulder.
func TestTOTP_AcceptsOneStepOfDrift(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}
	now := time.Now()
	for _, drift := range []time.Duration{-totpStep, 0, totpStep} {
		code := totpAt(secret, now.Add(drift), totpDigits)
		if !verifyTOTPAt(secret, code, now) {
			t.Errorf("a code %v away was refused", drift)
		}
	}
	for _, drift := range []time.Duration{-3 * totpStep, 3 * totpStep} {
		code := totpAt(secret, now.Add(drift), totpDigits)
		if verifyTOTPAt(secret, code, now) {
			t.Errorf("a code %v away was accepted", drift)
		}
	}
}

func TestTOTP_RejectsMalformedCodes(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}
	now := time.Now()
	for _, code := range []string{"", "12345", "1234567", "abcdef", "12 34 56", "------"} {
		if verifyTOTPAt(secret, code, now) {
			t.Errorf("malformed code %q verified", code)
		}
	}
	// An empty secret is the state before enrolment. It must not be a state in
	// which every code works.
	if verifyTOTPAt("", "000000", now) {
		t.Error("an empty secret verified a code")
	}
}

// The enrolment URI is what the QR encodes, and an app that cannot parse it is
// an app the operator cannot enrol with.
func TestTOTP_EnrolmentURIIsWellFormed(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	uri := TOTPEnrolmentURI("alice", "panel.example.com", secret)

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parsing the enrolment URI: %v", err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Errorf("URI = %q, want an otpauth://totp/ URI", uri)
	}
	// The label carries issuer:account, which is what the app shows in its
	// list. Without the issuer, three servers all read "alice".
	if !strings.HasPrefix(parsed.Path, "/panel.example.com:alice") {
		t.Errorf("label = %q, want issuer:account", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("secret") != secret {
		t.Errorf("secret = %q", q.Get("secret"))
	}
	if q.Get("issuer") != "panel.example.com" {
		t.Errorf("issuer = %q", q.Get("issuer"))
	}
	if q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Errorf("parameters = %v, want the defaults every app assumes", q)
	}
}

// A panel with no domain yet still has to enrol, so the issuer falls back to
// something rather than producing an empty label.
func TestTOTP_EnrolmentURIHasAnIssuerWithoutADomain(t *testing.T) {
	uri := TOTPEnrolmentURI("alice", "", "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if parsed.Query().Get("issuer") == "" {
		t.Error("the enrolment URI has no issuer, so an app will list it namelessly")
	}
}

// The secret is base32 without padding, which is what authenticator apps
// accept when it is typed by hand rather than scanned.
func TestTOTP_SecretIsTypeableBase32(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		secret, err := NewTOTPSecret()
		if err != nil {
			t.Fatalf("NewTOTPSecret: %v", err)
		}
		if strings.Contains(secret, "=") {
			t.Errorf("secret %q is padded, which some apps refuse when typed", secret)
		}
		if strings.Trim(secret, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
			t.Errorf("secret %q is not base32", secret)
		}
		if len(secret) < 32 {
			t.Errorf("secret %q is %d characters, under 160 bits", secret, len(secret))
		}
		if seen[secret] {
			t.Fatal("two secrets were identical")
		}
		seen[secret] = true
	}
}
