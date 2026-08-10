// Package staging makes a copy of a live site that is safe to have on the
// internet: not indexed, behind a password, on its own database, with its own
// certificate.
//
// It is a site like any other. Everything servlo does to a site — deploys,
// backups, workers, cron, its own PHP version — works here unchanged, and that
// is the point of building it this way rather than as a mode. What the staging
// block adds is which live site it copies from, and the two guards that are not
// optional.
package staging

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/nginx"
	"golang.org/x/crypto/bcrypt"
)

// DefaultUser is who logs in. One account for the site rather than one per
// person: this is a door, not an identity, and the people who need through it
// are the same people who can already read the panel's audit log.
const DefaultUser = "staging"

// passwordBytes is how much randomness the generated password carries. Twenty
// four bytes is 32 base64 characters, which nobody types and nobody guesses.
//
// Generated rather than chosen, always. A staging password an operator picked
// is the same password as the last five staging sites, and the whole value of
// this is that the copy of the client's data behind it is not reachable by
// somebody who guessed.
const passwordBytes = 24

// bcryptCost is the work factor. Ten is bcrypt's own default and is checked by
// nginx on every request to the site, so a higher one would be paid by the
// developer reloading a page rather than by an attacker.
const bcryptCost = 10

// Credentials are what a person is told once.
type Credentials struct {
	User string
	// Password is only ever in memory and in the one response that created it.
	// Nothing stores it, which is deliberate: a staging password kept
	// somewhere readable is the password on a copy of production data kept
	// somewhere readable.
	Password string
}

// NewCredentials generates a login for a staging site and returns the hash to
// store beside it.
func NewCredentials(user string) (Credentials, string, error) {
	if user = strings.TrimSpace(user); user == "" {
		user = DefaultUser
	}
	if err := validUser(user); err != nil {
		return Credentials{}, "", err
	}
	raw := make([]byte, passwordBytes)
	if _, err := rand.Read(raw); err != nil {
		return Credentials{}, "", fmt.Errorf("generating a password: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(raw)

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return Credentials{}, "", err
	}
	return Credentials{User: user, Password: password}, string(hash), nil
}

// validUser refuses a name that would not survive being written into an
// htpasswd file, where the first colon ends the name and a newline ends the
// record. A name carrying either would authorise something nobody asked for.
func validUser(user string) error {
	if strings.ContainsAny(user, ":\n\r\x00") {
		return fmt.Errorf("a staging username cannot contain a colon or a newline")
	}
	if len(user) > 64 {
		return fmt.Errorf("that username is too long")
	}
	return nil
}

// WriteHtpasswd puts the credentials where nginx reads them.
//
// bcrypt rather than apr1. nginx checks anything that is not apr1 through the
// C library's crypt, and the nginx image servlo runs is Alpine, whose musl does
// support bcrypt. apr1 is MD5 and would be the wrong thing to write down for a
// door in front of a copy of a client's database.
func WriteHtpasswd(domain, user, hash string) error {
	if err := validUser(user); err != nil {
		return err
	}
	if err := os.MkdirAll(config.NginxHtpasswdDir(), 0755); err != nil {
		return err
	}
	// 0644 rather than 0600: nginx reads this from its own container as its own
	// user, and it holds a bcrypt hash rather than a password. A file nginx
	// cannot read makes the site answer 500 to everybody, which is a worse
	// outcome than a hash being readable by an account that can already read
	// the site's .env.
	return os.WriteFile(nginx.HtpasswdPath(domain), []byte(user+":"+hash+"\n"), 0644)
}

// RemoveHtpasswd takes the credentials away, for a site that stopped being a
// staging site or stopped existing.
func RemoveHtpasswd(domain string) {
	_ = os.Remove(nginx.HtpasswdPath(domain))
}
