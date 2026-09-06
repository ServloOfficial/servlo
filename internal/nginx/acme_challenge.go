package nginx

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ServloOfficial/servlo/internal/config"
)

// acmeChallengeRoot is where the shared challenge webroot is mounted inside the
// nginx container. The host side is config.ACMEChallengeDir(); the two are
// different paths on different sides of a bind mount, and a test pins them to
// the quadlet so they cannot drift apart silently.
const acmeChallengeRoot = "/etc/nginx/acme-challenge"

// acmeChallengePrefix is the URL path Let's Encrypt fetches, fixed by RFC 8555.
const acmeChallengePrefix = "/.well-known/acme-challenge/"

// acmeChallengeLocation is the block every vhost carries. `^~` makes it win
// over any regex location the site or its framework snippet declares, so a
// framework that routes everything through index.php cannot swallow the
// challenge. `root` rather than `alias` because nginx appends the whole URI to
// root, which means the token path on disk mirrors the URL exactly and there is
// one layout to reason about rather than two.
//
// try_files ... =404 keeps a missing token a plain 404: the ACME server treats
// that as "not ready" and retries, where falling through to the site would hand
// it an HTML page and fail the authorization outright.
const acmeChallengeLocation = `    location ^~ ` + acmeChallengePrefix + ` {
%s        root ` + acmeChallengeRoot + `;
        default_type "text/plain";
        try_files $uri =404;
    }
`

// ACMEChallengeLocation returns the block for the vhost templates.
func ACMEChallengeLocation() string { return fmt.Sprintf(acmeChallengeLocation, "") }

// challengeTokenPath is where a token for the given name is written on the host.
// It mirrors the URL under the webroot because the location above uses `root`.
func challengeTokenPath(token string) string {
	return filepath.Join(config.ACMEChallengeDir(), filepath.FromSlash(acmeChallengePrefix), token)
}

// WriteChallengeToken publishes one HTTP-01 key authorization so nginx can
// serve it. The file is world-readable on purpose: nginx runs in its own
// container as its own user, and the token is a public value the ACME server is
// about to fetch over plain HTTP anyway.
func WriteChallengeToken(token, keyAuth string) error {
	path := challengeTokenPath(token)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(keyAuth), 0644)
}

// RemoveChallengeToken deletes a published token. Called on the way out of an
// issuance, success or failure: a token left behind is a stale public file, and
// on the next issuance for the same name it would be indistinguishable from the
// fresh one.
func RemoveChallengeToken(token string) {
	os.Remove(challengeTokenPath(token)) //nolint:errcheck
}

// EnsureChallengeDir creates the webroot so the nginx container has something to
// mount. Podman creates a missing bind source as a root-owned directory, which
// the panel then cannot write tokens into, so this has to exist first.
func EnsureChallengeDir() error {
	return os.MkdirAll(filepath.Join(config.ACMEChallengeDir(), filepath.FromSlash(acmeChallengePrefix)), 0755)
}
