package authz

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 specifies SHA-1, and every authenticator app assumes it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP, per RFC 6238.
//
// SHA-1 with six digits on a thirty-second step is not a choice so much as the
// only interoperable option: it is what Google Authenticator, 1Password, Aegis
// and the rest assume when they scan a QR code, and a stronger hash produces
// codes no app will generate. The security here comes from the second factor
// existing at all, not from the digest.
//
// Optional, per the PRD. A panel behind a strong passphrase and a rate limiter
// is already hard to guess; TOTP is for the operator who wants a stolen
// password not to be enough on its own.

const (
	totpDigits = 6
	totpStep   = 30 * time.Second
	// totpSkew is one step either side. A phone clock is rarely exact and a
	// code typed as the window turns over should still work. Wider would be a
	// longer life for a code read over someone's shoulder.
	totpSkew = 1
	// totpSecretBytes is 160 bits, the RFC's recommendation and the length
	// every app handles without comment.
	totpSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh base32 secret, unpadded so it can be typed by
// hand into an app that will not scan.
func NewTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating a TOTP secret: %w", err)
	}
	return totpEncoding.EncodeToString(raw), nil
}

// TOTPEnrolmentURI is the otpauth:// URI an authenticator app reads from the QR
// code. The parameters are spelled out rather than left to default, because
// apps disagree about what the defaults are.
func TOTPEnrolmentURI(account, issuer, secret string) string {
	if issuer == "" {
		// A panel with no domain yet still has to enrol, and a nameless entry
		// in someone's app is one they cannot tell from any other.
		issuer = "Servlo"
	}
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", int(totpStep.Seconds())))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}

// VerifyTOTP reports whether code is valid for secret right now.
func VerifyTOTP(secret, code string) bool {
	_, ok := VerifyTOTPCounter(secret, code)
	return ok
}

// VerifyTOTPCounter is VerifyTOTP with the step the code belongs to, which is
// what makes a code spendable once.
//
// RFC 6238 section 5.2 asks for exactly this: a verifier must not accept a code
// it has already accepted. A code is good for its own step and one either side,
// so without the counter one read off a screen, out of a phishing page or out of
// a form somebody logged is good for another minute and a half, against an
// account whose password is already known. Recording the step it belongs to and
// refusing anything at or below it is what closes that.
func VerifyTOTPCounter(secret, code string) (int64, bool) {
	return verifyTOTPCounterAt(secret, code, time.Now())
}

func verifyTOTPAt(secret, code string, now time.Time) bool {
	_, ok := verifyTOTPCounterAt(secret, code, now)
	return ok
}

func verifyTOTPCounterAt(secret, code string, now time.Time) (int64, bool) {
	if secret == "" || len(code) != totpDigits {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	// Every candidate is checked even after a match, so the time taken does not
	// say which step matched.
	matched := false
	var counter int64
	for skew := -totpSkew; skew <= totpSkew; skew++ {
		at := now.Add(time.Duration(skew) * totpStep)
		want := totpAt(secret, at, totpDigits)
		if subtle.ConstantTimeCompare([]byte(code), []byte(want)) == 1 {
			matched = true
			counter = counterAt(at)
		}
	}
	return counter, matched
}

// counterAt is the step number a moment falls in, which is what a code is
// really a function of.
func counterAt(at time.Time) int64 {
	return at.Unix() / int64(totpStep.Seconds())
}

// totpAt is the RFC 6238 code for a secret at a moment.
func totpAt(secret string, at time.Time, digits int) string {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counterAt(at)))

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation, RFC 4226 §5.3: the low nibble of the last byte picks
	// where in the digest to read the code from.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod)
}
