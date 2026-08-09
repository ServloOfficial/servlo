// Package sshkeys manages the authorised keys for the account servlo runs as.
//
// This is the one piece of server access servlo owns outright. ufw, fail2ban
// and sshd's own configuration all need root, so servlo reports them and prints
// commands; authorized_keys belongs to this user, so servlo can just edit it.
//
// Additively, always. Every write keeps every key servlo did not put there,
// because that file is how the operator gets in, and a panel that rewrote it
// wholesale would eventually lock somebody out of their own server from a
// browser tab. And password authentication is never touched: servlo does not
// edit sshd_config at all (CLAUDE.md §3.2).
package sshkeys

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Key is one authorised key.
type Key struct {
	// Type is the algorithm, ssh-ed25519 and so on.
	Type string `json:"type"`
	// Comment is the tail of the line, usually who it belongs to. It is the
	// only human-readable handle a key has, which is why a key added without
	// one is refused.
	Comment string `json:"comment"`
	// Fingerprint is the SHA256 form ssh-keygen -l prints, so a key can be
	// checked against what somebody says they sent without the whole blob.
	Fingerprint string `json:"fingerprint"`
	// Line is the file's own text, kept so removing a key removes exactly the
	// line that was read rather than one servlo reconstructed.
	Line string `json:"-"`
	// Options is anything before the type, such as a from= restriction. Kept so
	// a rewrite never quietly drops one.
	Options string `json:"options,omitempty"`
}

// keyTypes are the algorithms a line may start with. Anything else is not a key
// servlo will write: an unknown first field means the line is something other
// than what it looks like, and appending it to authorized_keys is how sshd ends
// up refusing to read the whole file.
var keyTypes = map[string]bool{
	"ssh-ed25519":                        true,
	"ssh-rsa":                            true,
	"ecdsa-sha2-nistp256":                true,
	"ecdsa-sha2-nistp384":                true,
	"ecdsa-sha2-nistp521":                true,
	"sk-ssh-ed25519@openssh.com":         true,
	"sk-ecdsa-sha2-nistp256@openssh.com": true,
}

// Path is the file. Not configurable: sshd reads exactly this one.
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "authorized_keys")
}

// List is every key currently authorised, in file order.
//
// Comments and blank lines are skipped rather than reported. A line servlo
// cannot parse is skipped too, because the alternative is refusing to show any
// keys because of one line somebody hand-edited.
func List() ([]Key, error) {
	f, err := os.Open(Path())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", Path(), err)
	}
	defer f.Close() //nolint:errcheck

	var out []Key
	scanner := bufio.NewScanner(f)
	// A key line is long; the default 64 KiB is enough but a certificate is
	// not, and a truncated line silently parsed as a key would be worse.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, ok := Parse(line); ok {
			out = append(out, key)
		}
	}
	return out, scanner.Err()
}

// Parse reads one authorized_keys line.
func Parse(line string) (Key, bool) {
	line = strings.TrimSpace(line)
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Key{}, false
	}
	// Options come before the type and may contain spaces inside quotes, so the
	// type is found by looking for the first field that is one rather than by
	// counting fields.
	at := -1
	for i, f := range fields {
		if keyTypes[f] {
			at = i
			break
		}
	}
	if at < 0 || at+1 >= len(fields) {
		return Key{}, false
	}
	blob := fields[at+1]
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return Key{}, false
	}
	sum := sha256.Sum256(raw)
	return Key{
		Type:        fields[at],
		Comment:     strings.Join(fields[at+2:], " "),
		Fingerprint: "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "="),
		Line:        line,
		Options:     strings.Join(fields[:at], " "),
	}, true
}

// Add authorises a key, keeping every key already there.
//
// A key already present is not an error and not a duplicate line: somebody
// pasting the same key twice means they want it to work, and two identical
// lines in authorized_keys is a file that looks tampered with.
func Add(line, comment string) (Key, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Key{}, fmt.Errorf("paste the whole public key, the line that starts ssh-ed25519 or ssh-rsa")
	}
	if strings.ContainsAny(line, "\n\r") {
		return Key{}, fmt.Errorf("that is more than one line. Add one key at a time, so removing one later removes what you meant")
	}
	if strings.HasPrefix(line, "-----BEGIN") {
		return Key{}, fmt.Errorf("that is a private key. Send the public one, the file ending .pub, and keep this one where it is")
	}
	key, ok := Parse(line)
	if !ok {
		return Key{}, fmt.Errorf("that is not a public key servlo recognises. It should start with a type such as ssh-ed25519 followed by the key itself")
	}
	if comment = strings.TrimSpace(comment); comment != "" {
		// The name ends up at the end of a line in authorized_keys, so a name
		// carrying a newline is a way to authorise a second key nobody
		// approved. It goes in as one line or not at all.
		if strings.ContainsAny(comment, "\n\r") {
			return Key{}, fmt.Errorf("a key's name has to be one line")
		}
		key.Comment = comment
	}
	if key.Comment == "" {
		// The only handle a key has. Without one, a list of five keys is five
		// identical rows and nobody dares remove any of them.
		return Key{}, fmt.Errorf("give the key a name, so whoever reads this list in a year knows whose it is")
	}

	existing, err := List()
	if err != nil {
		return Key{}, err
	}
	for _, e := range existing {
		if e.Fingerprint == key.Fingerprint {
			return e, nil
		}
	}
	// Rebuilt rather than written through, so the name in the list is the name
	// in the file and a comment carrying a newline cannot reach it.
	key.Line = strings.TrimSpace(key.Options + " " + key.Type + " " + blobOf(line, key.Type) + " " + key.Comment)
	return key, appendKey(key)
}

// blobOf returns the key material that follows the type on a line.
func blobOf(line, keyType string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		if f == keyType && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// appendKey writes one key onto the end, creating the file and its directory with
// the modes sshd insists on.
//
// sshd refuses to read an authorized_keys that anyone but its owner can write,
// and the failure is a silent refusal of the key rather than an error anyone
// sees, so the modes are set on every write rather than assumed.
func appendKey(key Key) error {
	path := Path()
	if path == "" {
		return fmt.Errorf("servlo cannot tell where this account's home directory is")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil && !os.IsNotExist(err) {
		return err
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next := string(existing)
	if next != "" && !strings.HasSuffix(next, "\n") {
		// A file whose last line has no newline would otherwise have the new
		// key appended to the end of it, breaking both.
		next += "\n"
	}
	next += key.Line + "\n"
	return write(path, next)
}

// Remove takes a key away by fingerprint, leaving every other line exactly as
// it was, comments included.
func Remove(fingerprint string) (bool, error) {
	path := Path()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var kept []string
	var removed bool
	for _, line := range strings.Split(string(raw), "\n") {
		key, ok := Parse(line)
		if ok && key.Fingerprint == fingerprint {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return false, nil
	}
	// Rejoin as read, so a trailing newline stays one and a hand-written
	// comment stays where its author put it.
	return true, write(path, strings.Join(kept, "\n"))
}

// write replaces the file through a temporary file and a rename, so an
// interrupted write leaves the previous keys rather than half a file. Locking
// yourself out because the disk filled up mid-write is not a failure mode worth
// having.
func write(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".authorized_keys-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) //nolint:errcheck

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
