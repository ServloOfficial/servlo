// Package deploykey mints the SSH key a site uses to clone and pull from a
// private repository.
//
// A deploy key rather than the operator's own key, and one per site rather than
// one per server. A deploy key is scoped to a single repository, so a site that
// gets compromised does not hand over an account that can read every repository
// its owner can; and per site means revoking one site's access is deleting one
// key rather than working out which other sites would break.
//
// Servlo generates it rather than asking for one, because the alternative is a
// form that asks an operator to paste a private key into a browser, and there
// is no version of that worth building.
package deploykey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/realrashid/servlo/internal/config"
)

// Key is a site's deploy key. Only the public half is ever returned to a
// browser; the private half is named by its path so callers can hand it to ssh
// without it passing through an API response.
type Key struct {
	// Public is the one-line authorized_keys form the operator pastes into the
	// repository's deploy keys.
	Public string `json:"public"`
	// PrivatePath is where the private half lives, 0600.
	PrivatePath string `json:"-"`
}

// safeSiteKey matches a domain that can name a file without naming a path.
var safeSiteKey = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// Dir is where deploy keys live: with servlo's other credentials, and never
// inside a site directory, which a deploy publishes to the world.
func Dir() string { return filepath.Join(config.DataDir(), "deploy-keys") }

func keyPath(site string) string { return filepath.Join(Dir(), site) }

// Ensure returns the site's deploy key, generating it the first time.
//
// It is idempotent on purpose. The operator reads the public key, pastes it
// into GitHub, and comes back to press test; a second call that minted a new
// key would invalidate what they just pasted and the failure would read like a
// GitHub problem.
func Ensure(site string) (Key, error) {
	site = strings.ToLower(strings.TrimSpace(site))
	if !safeSiteKey.MatchString(site) {
		return Key{}, fmt.Errorf("%q is not a usable site name for a deploy key", site)
	}

	path := keyPath(site)
	if existing, err := os.ReadFile(path + ".pub"); err == nil {
		return Key{Public: strings.TrimSpace(string(existing)), PrivatePath: path}, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Key{}, fmt.Errorf("generating a deploy key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return Key{}, fmt.Errorf("encoding the deploy key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return Key{}, fmt.Errorf("encoding the deploy key: %w", err)
	}
	// The comment is what an operator reads in a GitHub account holding six of
	// these, so it names the site rather than a user@host nobody chose.
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " servlo-" + site

	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return Key{}, err
	}
	// Written 0600 by the open rather than chmodded after, so it is never
	// briefly readable by anything that happened to be watching.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return Key{}, err
	}
	if _, err := f.Write(pem.EncodeToMemory(block)); err != nil {
		f.Close()
		return Key{}, err
	}
	if err := f.Close(); err != nil {
		return Key{}, err
	}
	if err := os.WriteFile(path+".pub", []byte(line+"\n"), 0o644); err != nil {
		return Key{}, err
	}
	return Key{Public: line, PrivatePath: path}, nil
}

// Remove deletes a site's deploy key. An absent key is not an error: a site
// added from a folder never had one.
func Remove(site string) error {
	site = strings.ToLower(strings.TrimSpace(site))
	if !safeSiteKey.MatchString(site) {
		return fmt.Errorf("%q is not a usable site name for a deploy key", site)
	}
	for _, p := range []string{keyPath(site), keyPath(site) + ".pub"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// SSHCommand is the GIT_SSH_COMMAND that makes git use this key and nothing
// else. IdentitiesOnly stops ssh offering every key in the agent first, which
// on a server with an agent loaded is how a clone succeeds under the wrong
// identity and nobody notices until the key is revoked.
func SSHCommand(privatePath string) string {
	return "ssh -i " + shellQuote(privatePath) + " -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
}

// authSuccessBanners are how the forges say "yes, I know this key". GitHub
// first, then GitLab and Bitbucket in their own words.
var authSuccessBanners = []string{
	"successfully authenticated",
	"welcome to gitlab",
	"logged in as",
}

// Authenticated reports whether output is a host's "yes, I know this key"
// answer.
//
// This is not a matter of exit status. GitHub refuses shell access and exits 1
// on a perfectly good key, so reading the exit code would report every working
// deploy key as broken.
func Authenticated(output string) bool {
	lower := strings.ToLower(output)
	for _, banner := range authSuccessBanners {
		if strings.Contains(lower, banner) {
			return true
		}
	}
	return false
}

// ExplainSSHFailure turns ssh's output into a sentence naming what to do about
// it. "Connection failed" is what this exists to avoid: every one of these
// means something different and every one has a different fix.
func ExplainSSHFailure(output string) string {
	trimmed := strings.TrimSpace(output)
	lower := strings.ToLower(trimmed)

	switch {
	case strings.Contains(lower, "permission denied (publickey"):
		return "the host refused the key: add the deploy key above to the repository's deploy keys, then test again"
	case strings.Contains(lower, "repository not found"):
		return "authentication worked but the repository was not found: check the clone URL, and that the deploy key is on that repository rather than another one"
	case strings.Contains(lower, "host key verification failed"):
		return "the host key was not accepted: the server has a different host key on record for this host than the one it presented"
	case strings.Contains(lower, "could not resolve hostname"):
		return "the host name did not resolve: check the spelling, and that this server has working DNS"
	case strings.Contains(lower, "connection timed out"), strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "network is unreachable"), strings.Contains(lower, "no route to host"):
		return "this server could not reach the host on port 22: check the outbound firewall"
	}

	// Nothing anticipated. The most useful thing left to say is what ssh said,
	// which beats a sentence servlo made up about a failure it does not know.
	if trimmed == "" {
		return "the connection failed and ssh printed nothing"
	}
	return "the connection failed: " + firstLines(trimmed, 3)
}

// firstLines keeps a failure readable in a modal without losing the cause,
// which is nearly always in the first line or two.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
