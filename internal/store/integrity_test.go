package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func digestOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

const laravelYAML = "name: laravel\nversion: \"13\"\n"

// A definition declares the commands servlo runs on deploy and the images it
// starts, so a swapped one is code execution on the droplet. When the index
// carries a digest, the body has to match it or it never reaches the parser.
func TestVerifyDigest_AcceptsAMatchingBody(t *testing.T) {
	if err := verifyDigest([]byte(laravelYAML), digestOf(laravelYAML), "laravel/13.yaml"); err != nil {
		t.Errorf("a matching body was rejected: %v", err)
	}
}

func TestVerifyDigest_RejectsATamperedBody(t *testing.T) {
	err := verifyDigest([]byte("name: evil\n"), digestOf(laravelYAML), "laravel/13.yaml")
	if err == nil {
		t.Fatal("a body that does not match its digest was accepted")
	}
	// The message has to name the file, since the operator's next question is
	// which definition to distrust.
	if !strings.Contains(err.Error(), "laravel/13.yaml") {
		t.Errorf("error %q does not name the definition", err)
	}
}

// An index entry with no digest is the ordinary case for a store published
// before digests existed, and for the embedded copy, which is compiled in and
// cannot be swapped remotely. It is allowed through rather than refused, so
// adding digests does not strand every existing install.
func TestVerifyDigest_NoDigestIsNotAFailure(t *testing.T) {
	if err := verifyDigest([]byte(laravelYAML), "", "laravel/13.yaml"); err != nil {
		t.Errorf("an entry with no digest was rejected: %v", err)
	}
}

// A digest servlo cannot evaluate must fail rather than pass: treating an
// unknown algorithm as "nothing to check" would let an attacker disable the
// check by naming one.
func TestVerifyDigest_RejectsAnAlgorithmItCannotCheck(t *testing.T) {
	if err := verifyDigest([]byte(laravelYAML), "md5:abcdef", "laravel/13.yaml"); err == nil {
		t.Error("an unknown digest algorithm was treated as no digest")
	}
}

func TestIndexEntry_DigestForVersion(t *testing.T) {
	e := IndexEntry{
		Name:     "laravel",
		Versions: []string{"13", "12"},
		Digests:  map[string]string{"13": digestOf(laravelYAML)},
	}
	if got := e.DigestFor("13"); got != digestOf(laravelYAML) {
		t.Errorf("DigestFor(13) = %q, want the recorded digest", got)
	}
	if got := e.DigestFor("12"); got != "" {
		t.Errorf("DigestFor(12) = %q, want empty for a version with none", got)
	}
}
