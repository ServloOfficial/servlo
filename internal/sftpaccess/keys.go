// Package sftpaccess is per-site SFTP: the keys servlo manages without root,
// and the one privileged step it will only ever print.
//
// SFTP on a Linux box is an OpenSSH subsystem, and OpenSSH is root's. Servlo is
// not root and does not ask to be (CLAUDE.md section 3.2), so the feature
// splits cleanly in two, and being honest about which half is which is most of
// what this package is for.
//
// The half servlo does: authorised keys. A public key goes into the servlo
// user's own ~/.ssh/authorized_keys, inside a marked block, with `restrict` so
// the session gets no pty, no forwarding and no shell, and with a forced
// `internal-sftp -d <site>` so it opens in the site's directory. Nothing here
// needs a privilege servlo does not already have, and nothing here touches a
// line servlo did not write. Password authentication is never mentioned, let
// alone disabled: this file only ever appends keys.
//
// The half servlo will not do: the chroot. Confining the session to the site so
// that `cd ..` cannot leave it is sshd's ChrootDirectory, the chroot must be
// owned by root and not writable by anyone else, and a non-root process cannot
// create such a directory. chroot.go generates that configuration and prints
// the sudo block; it never runs it.
//
// What none of this is: isolation between sites. Every site on the machine runs
// as the same Linux user (PRD section 6), so an SFTP session confined to one
// site is confined by a path check and a kernel chroot, not by a user boundary.
// If the same person also has panel access, or can get PHP to run in any site,
// they have the whole account. The docs say this in as many words, because a
// feature called "locked to that site's directory" that is quietly weaker than
// it sounds is worse than no feature.
package sftpaccess

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ServloOfficial/servlo/internal/atomicfile"

	"golang.org/x/crypto/ssh"
)

// The markers around servlo's own lines. Everything between them belongs to
// servlo and is rewritten whole; everything outside them is somebody else's and
// is copied through byte for byte.
const (
	beginMarker = "# BEGIN servlo sftp - managed by servlo, do not edit inside this block"
	endMarker   = "# END servlo sftp"
)

// authorizedKeysPath is a var so tests can point it somewhere that is not the
// running user's real one.
var authorizedKeysPath = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "authorized_keys"), nil
}

// Key is one public key authorised for one site.
type Key struct {
	Site  string `json:"site"`
	Label string `json:"label"`
	Type  string `json:"type"`
	// Fingerprint is the SHA256 form ssh-keygen prints, which is what an
	// operator has in front of them when they want to know if this is the key
	// they think it is.
	Fingerprint string `json:"fingerprint"`
	// SitePath is the directory the session opens in.
	SitePath string `json:"site_path"`
	// blob is the base64 body of the key. Unexported, so it never reaches an
	// API response: the panel identifies a key by its fingerprint, and a
	// response is a place things get copied out of.
	blob string
}

// labelPattern is what a label may be. Letters, digits, dash, dot, underscore
// and nothing else: the label is written into the file verbatim, and a label
// with a newline in it is a second authorized_keys line that servlo did not
// write and did not restrict.
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Add authorises a public key for one site.
func Add(site, sitePath, label, publicKey string) (Key, error) {
	if site == "" {
		return Key{}, fmt.Errorf("a key has to be for a site")
	}
	if !labelPattern.MatchString(label) {
		return Key{}, fmt.Errorf("a label may be up to 64 letters, digits, dots, dashes or underscores, and nothing else")
	}
	if err := checkSitePath(sitePath); err != nil {
		return Key{}, err
	}
	parsed, err := parsePublicKey(publicKey)
	if err != nil {
		return Key{}, err
	}
	key := Key{
		Site:        site,
		Label:       label,
		Type:        parsed.Type(),
		Fingerprint: ssh.FingerprintSHA256(parsed),
		SitePath:    sitePath,
		blob:        base64.StdEncoding.EncodeToString(parsed.Marshal()),
	}

	existing, err := List()
	if err != nil {
		return Key{}, err
	}
	for _, have := range existing {
		if have.Fingerprint == key.Fingerprint {
			return Key{}, fmt.Errorf("that key is already authorised for %s", have.Site)
		}
	}
	if err := write(append(existing, key)); err != nil {
		return Key{}, err
	}
	return key, nil
}

// Remove withdraws one key by fingerprint. It reports whether there was one.
func Remove(fingerprint string) (bool, error) {
	existing, err := List()
	if err != nil {
		return false, err
	}
	kept := make([]Key, 0, len(existing))
	found := false
	for _, key := range existing {
		if key.Fingerprint == fingerprint {
			found = true
			continue
		}
		kept = append(kept, key)
	}
	if !found {
		return false, nil
	}
	return true, write(kept)
}

// List returns every key servlo manages, in the order they appear in the file.
func List() ([]Key, error) {
	path, err := authorizedKeysPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, managed, _ := split(string(data))
	var keys []Key
	for _, line := range managed {
		if key, ok := parseLine(line); ok {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// ForSite is List filtered to one site.
func ForSite(site string) ([]Key, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}
	var out []Key
	for _, key := range all {
		if key.Site == site {
			out = append(out, key)
		}
	}
	return out, nil
}

// write rewrites servlo's block and leaves everything else exactly as it was.
func write(keys []Key) error {
	path, err := authorizedKeysPath()
	if err != nil {
		return err
	}
	var before, after []string
	if data, err := os.ReadFile(path); err == nil {
		before, _, after = split(string(data))
	} else if !os.IsNotExist(err) {
		return err
	}

	lines := append([]string{}, before...)
	if len(keys) > 0 {
		lines = append(lines, beginMarker)
		for _, key := range keys {
			lines = append(lines, key.line())
		}
		lines = append(lines, endMarker)
	}
	lines = append(lines, after...)

	// 0700 on the directory and 0600 on the file, because sshd refuses a
	// group-writable authorized_keys and says so only in its own log.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Replaced by rename rather than rewritten in place. Everything outside
	// servlo's block belongs to the operator, servlo holds no copy of it, and a
	// write that runs out of disk halfway would take it with it. The rename also
	// means sshd, which reads this file on every connection, can never catch it
	// half written and reject a key that is really there.
	body := strings.Join(trimTrailingBlanks(lines), "\n") + "\n"
	return atomicfile.Write(path, []byte(body), 0o600)
}

// line is the authorized_keys entry for one key.
//
// `restrict` first, so the session has no pty, no port forwarding, no agent
// forwarding and no X11. The forced command is the sftp subsystem and nothing
// else, which is what makes this file transfer rather than a way onto the box.
func (k Key) line() string {
	return fmt.Sprintf("restrict,command=%q %s %s servlo-sftp site=%s label=%s",
		"internal-sftp -d "+k.SitePath, k.Type, k.blob, k.Site, k.Label)
}

// parseLine reads one of servlo's own lines back.
func parseLine(line string) (Key, bool) {
	if !strings.HasPrefix(line, "restrict,") {
		return Key{}, false
	}
	marker := strings.Index(line, " servlo-sftp ")
	if marker < 0 {
		return Key{}, false
	}
	fields := strings.Fields(line[marker:])
	key := Key{}
	for _, field := range fields[1:] {
		name, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch name {
		case "site":
			key.Site = value
		case "label":
			key.Label = value
		}
	}
	if key.Site == "" {
		return Key{}, false
	}
	// The key itself is re-parsed rather than trusted, so a hand-edited line
	// cannot make List report a fingerprint that is not the key's. This one
	// keeps its options, unlike the input path: servlo wrote them.
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(line[:marker])))
	if err != nil {
		return Key{}, false
	}
	key.Type = parsed.Type()
	key.Fingerprint = ssh.FingerprintSHA256(parsed)
	key.blob = base64.StdEncoding.EncodeToString(parsed.Marshal())
	if start := strings.Index(line, `command="internal-sftp -d `); start >= 0 {
		rest := line[start+len(`command="internal-sftp -d `):]
		if end := strings.Index(rest, `"`); end >= 0 {
			key.SitePath = rest[:end]
		}
	}
	return key, true
}

// parsePublicKey accepts one public key in authorized_keys form and refuses a
// line that carries its own options, which would be the caller writing their
// own entry through servlo's.
func parsePublicKey(text string) (ssh.PublicKey, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("paste the public key, the one line that starts ssh-ed25519 or ssh-rsa")
	}
	if strings.ContainsAny(text, "\n\r") {
		return nil, fmt.Errorf("a public key is one line; that is more than one")
	}
	parsed, _, options, _, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("that does not look like an SSH public key: %w", err)
	}
	if len(options) > 0 {
		return nil, fmt.Errorf("paste the key on its own; servlo writes the options that confine it")
	}
	return parsed, nil
}

// checkSitePath refuses a path that would not survive being quoted into the
// forced command.
func checkSitePath(sitePath string) error {
	if sitePath == "" || !filepath.IsAbs(sitePath) {
		return fmt.Errorf("the site directory has to be an absolute path")
	}
	if strings.ContainsAny(sitePath, "\"'\\\n\r") {
		return fmt.Errorf("servlo will not write a site directory containing a quote, a backslash or a newline into sshd's configuration")
	}
	return nil
}

// split cuts a file into what is before servlo's block, the block's own lines,
// and what is after. A file with no block is all "before".
func split(content string) (before, managed, after []string) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	state := 0
	for _, line := range lines {
		switch {
		case state == 0 && line == beginMarker:
			state = 1
		case state == 1 && line == endMarker:
			state = 2
		case state == 0:
			before = append(before, line)
		case state == 1:
			managed = append(managed, line)
		default:
			after = append(after, line)
		}
	}
	return before, managed, after
}

// trimTrailingBlanks keeps the file from growing a blank line every rewrite.
func trimTrailingBlanks(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
