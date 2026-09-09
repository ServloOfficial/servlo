package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Service passwords, and why they are generated rather than written down.
//
// The presets inherited from upstream shipped a literal password, the same one
// on every install. On a development laptop that is a convenience. On a server
// hosting several people's sites it means every site's database shares a
// credential that is published in a public repository, so one site's code
// reading its own config learns how to reach every other site's data.
//
// The password is generated once per install, stored 0600, and substituted into
// the preset through the same {{...}} mechanism the ports and tags use. It is
// per install rather than per service because the presets cross-reference each
// other: phpMyAdmin has to log into MySQL, RedisInsight into Redis, and a
// per-service secret would need a resolution pass those definitions have no way
// to express.

const servicePasswordBytes = 24

var servicePasswordMu sync.Mutex

// ServicePasswordFile is where the generated password lives.
func ServicePasswordFile() string {
	return filepath.Join(ConfigDir(), "service-password")
}

// ServicePassword returns this install's service password, generating it on
// first use. It never returns empty: a caller that got one would substitute a
// blank password into a database's environment, which for MySQL means an
// account with no password at all.
func ServicePassword() (string, error) {
	servicePasswordMu.Lock()
	defer servicePasswordMu.Unlock()

	if data, err := os.ReadFile(ServicePasswordFile()); err == nil {
		if pw := string(data); len(pw) >= 16 {
			return pw, nil
		}
		// Too short to be one servlo generated. Rather than trust it, fall
		// through and replace it: a truncated write is the likely cause and a
		// weak database password is not worth preserving.
	} else if !os.IsNotExist(err) {
		return "", err
	}

	pw, err := generateServicePassword()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
		return "", err
	}
	// Staged and renamed, so a write that runs out of disk cannot leave a
	// truncated password behind: the file is created 0600 and only ever
	// appears at the real path complete.
	if err := writeFileAtomic(ServicePasswordFile(), []byte(pw), 0600); err != nil {
		return "", err
	}
	return pw, nil
}

// passwordPlaceholder is what a preset writes wherever a credential belongs.
// Bare braces are a YAML flow mapping rather than a string, so a scalar use has
// to be quoted: PASSWORD: "{{password}}".
const passwordPlaceholder = "{{password}}"

// substitutePassword replaces the placeholder throughout raw preset YAML.
//
// It runs on the bytes, before parsing, so the password reaches every corner of
// a definition — container environment, the vars written into a site's .env,
// connection URLs, the command a service is launched with, mounted config files
// — without each field needing its own line of substitution code. A field added
// to a preset tomorrow is covered the day it is added.
//
// Unlike {{name}} and {{tag}}, this placeholder depends on nothing about the
// version being resolved, which is what makes doing it this early correct.
func substitutePassword(data []byte) []byte {
	if !bytes.Contains(data, []byte(passwordPlaceholder)) {
		return data
	}
	pw, err := ServicePassword()
	if err != nil {
		// Leaving the placeholder produces a service that visibly fails to
		// start. Substituting nothing produces one that starts with no
		// password at all, which for MySQL is an open root account.
		return data
	}
	return bytes.ReplaceAll(data, []byte(passwordPlaceholder), []byte(pw))
}

// generateServicePassword returns a URL-safe random string.
//
// URL-safe matters more than it looks: these passwords land in connection URLs
// like postgresql://user:PASSWORD@host/db, and a password containing a colon,
// slash or at-sign silently produces a URL that parses into the wrong pieces.
// base64url over 24 bytes gives 192 bits with no character that needs escaping.
func generateServicePassword() (string, error) {
	buf := make([]byte, servicePasswordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating a service password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
