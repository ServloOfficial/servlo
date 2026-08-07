// Package authz is the panel's authentication and authorisation: password
// hashing, sessions, per-IP rate limiting, CSRF and (from S5.4) roles.
//
// Servlo inherited a model built for a laptop: loopback is trusted absolutely,
// and a LAN client presents HTTP Basic credentials over plain HTTP. On a
// droplet neither half holds. The panel is on the internet, so there is no
// trusted side of the connection, and Basic auth means the password crosses the
// wire on every request with no way to sign out and no way to see who is signed
// in.
package authz

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id parameters, and how to change them.
//
// These are the OWASP second recommendation (46 MiB, one iteration, one lane
// scaled to two): enough memory that a GPU's advantage largely evaporates,
// cheap enough that a login on a 1 GB droplet is not a denial of service
// against itself. Memory is the parameter that matters; raising time while
// leaving memory low buys much less than it looks.
//
// Raising them later is safe and needs no migration: every hash records the
// parameters it was made with, so old passwords keep verifying, and NeedsRehash
// tells the caller to store a stronger one the next time it holds the plaintext.
const (
	argonMemory  = 47104 // 46 MiB
	argonTime    = 1
	argonThreads = 2
	argonSaltLen = 16
	argonKeyLen  = 32
)

var errMalformedHash = errors.New("not a hash servlo can verify")

// HashPassword returns a PHC-format Argon2id hash of password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating a salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the stored hash.
//
// Anything it cannot parse is false. That sounds obvious and is the single most
// important line here: a parse failure that fell through to "match" would be an
// authentication bypass reachable by corrupting one field of a config file.
func VerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	if isBcryptHash(hash) {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	params, salt, want, err := decodeArgonHash(hash)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NeedsRehash reports whether a stored hash should be replaced the next time
// the plaintext is in hand: an inherited bcrypt hash, or an Argon2id hash made
// with parameters weaker than the current ones.
func NeedsRehash(hash string) bool {
	if hash == "" {
		return false
	}
	if isBcryptHash(hash) {
		return true
	}
	params, _, _, err := decodeArgonHash(hash)
	if err != nil {
		// Unparseable, so it cannot be verified against either. Saying it
		// wants rehashing is the answer that leads somewhere.
		return true
	}
	return params.memory < argonMemory || params.time < argonTime || params.threads < argonThreads
}

// isBcryptHash reports whether hash is one of the bcrypt forms upstream stored.
func isBcryptHash(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$")
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

// decodeArgonHash splits a PHC string into its parameters, salt and key.
func decodeArgonHash(hash string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, errMalformedHash
	}
	if version != argon2.Version {
		// A hash from a different Argon2 version would need that version's
		// implementation to check. Refusing is honest; guessing is not.
		return argonParams{}, nil, nil, errMalformedHash
	}

	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, errMalformedHash
	}
	if p.memory == 0 || p.time == 0 || p.threads == 0 {
		// argon2.IDKey panics on a zero parameter, so this is a crash as well
		// as a nonsense hash.
		return argonParams{}, nil, nil, errMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return argonParams{}, nil, nil, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return argonParams{}, nil, nil, errMalformedHash
	}
	return p, salt, key, nil
}
