package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

// Integrity of a fetched definition, and what it is and is not.
//
// A framework definition declares the commands servlo runs on deploy and the
// images it starts, so a swapped one is remote code execution on the droplet.
// The index carries a sha256 for each file it lists, and a body that does not
// match its digest never reaches the parser.
//
// This is integrity, not authenticity. It detects a definition that was
// corrupted or replaced independently of the index: a broken mirror, a
// truncated download, a tampered file behind a SERVLO_STORE_BASE_URL override.
// It does not defend against someone who controls the index itself, because
// they would simply publish a matching digest. Guarding that needs a signature
// over the index with a key servlo does not hold, so it is deliberately not
// claimed here; the check below is the seam such a signature would slot into.
//
// The strongest guarantee remains the embedded copy: every binary carries the
// definitions it shipped with, compiled in, and no fetch can replace them.

const sha256Prefix = "sha256:"

// verifyDigest checks body against the digest an index entry recorded for it.
// An empty digest is not a failure: the embedded store carries none, and a
// store published before digests existed carries none either, so requiring one
// would strand every existing install. A digest servlo cannot evaluate is a
// failure, because treating it as absent would let an attacker turn the check
// off by naming an algorithm.
func verifyDigest(body []byte, digest, path string) error {
	if digest == "" {
		return nil
	}
	if !strings.HasPrefix(digest, sha256Prefix) {
		return fmt.Errorf("%s: unsupported digest %q, refusing to use a definition servlo cannot verify", path, digest)
	}
	want, err := hex.DecodeString(strings.TrimPrefix(digest, sha256Prefix))
	if err != nil {
		return fmt.Errorf("%s: malformed digest %q", path, digest)
	}
	sum := sha256.Sum256(body)
	if subtle.ConstantTimeCompare(sum[:], want) != 1 {
		return fmt.Errorf("%s: content does not match the digest the index records (got sha256:%s), refusing to use it", path, hex.EncodeToString(sum[:]))
	}
	return nil
}

// DigestFor returns the digest the index records for one version of this
// framework, or "" when it records none.
func (e IndexEntry) DigestFor(version string) string {
	return e.Digests[version]
}
