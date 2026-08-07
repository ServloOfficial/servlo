package authz

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Recovery codes, for the phone that fell in a river.
//
// Without them, turning TOTP on is a way to lock yourself out of your own
// server, and the way back is a shell on it. The CLI reset path exists too, but
// it needs the machine; a recovery code works from anywhere, which is the point
// when the machine is what you were trying to reach.
//
// Each is one-time. A code that keeps working is a password written on a piece
// of paper, and the whole reason to hand out ten is that each one is spent.

const (
	recoveryCodeCount = 10
	// Two groups of five, which reads back off a screen without losing your
	// place. 10 characters from a 27-symbol alphabet is about 47 bits, far past
	// guessable given the login is rate limited.
	recoveryGroupSize   = 5
	recoveryGroupCount  = 2
	recoveryCodeLetters = "abcdefghjkmnpqrstuvwxyz23456789"
)

// NewRecoveryCodes returns a fresh batch: the codes to show the operator once,
// and the hashes to store.
func NewRecoveryCodes() (codes []string, hashes []string, err error) {
	seen := map[string]bool{}
	for len(codes) < recoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// ConsumeRecoveryCode checks code against the stored hashes and returns the set
// with the matching one removed.
//
// A plain SHA-256 rather than Argon2id, unlike a password: these are 47 bits of
// uniform randomness with no dictionary to try, so the slow hash would buy
// nothing and cost 46 MiB per attempt on a box someone can aim attempts at.
func ConsumeRecoveryCode(hashes []string, code string) ([]string, bool) {
	normalised := normaliseRecoveryCode(code)
	if normalised == "" {
		return hashes, false
	}
	want := hashRecoveryCode(normalised)
	for i, hash := range hashes {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(want)) != 1 {
			continue
		}
		remaining := make([]string, 0, len(hashes)-1)
		remaining = append(remaining, hashes[:i]...)
		remaining = append(remaining, hashes[i+1:]...)
		return remaining, true
	}
	return hashes, false
}

func newRecoveryCode() (string, error) {
	groups := make([]string, recoveryGroupCount)
	for g := range groups {
		var b strings.Builder
		for i := 0; i < recoveryGroupSize; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(recoveryCodeLetters))))
			if err != nil {
				return "", fmt.Errorf("generating a recovery code: %w", err)
			}
			b.WriteByte(recoveryCodeLetters[n.Int64()])
		}
		groups[g] = b.String()
	}
	return strings.Join(groups, "-"), nil
}

// normaliseRecoveryCode forgives what a human changes between reading a code off
// a screen and typing it back: case, surrounding space, and the separator.
func normaliseRecoveryCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	if len(code) != recoveryGroupSize*recoveryGroupCount {
		return ""
	}
	// Re-inserted so the stored hash is of one canonical form.
	var b strings.Builder
	for i := 0; i < recoveryGroupCount; i++ {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(code[i*recoveryGroupSize : (i+1)*recoveryGroupSize])
	}
	return b.String()
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normaliseRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}
