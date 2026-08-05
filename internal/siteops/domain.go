package siteops

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// A site has two names, and keeping them apart is the point of this file.
//
// The handle is internal: it names systemd units, containers and config keys,
// so it has to be a single safe label and is derived from the directory. The
// domain is what the world resolves and what a certificate is issued for, so it
// is given by the operator and never guessed. Servlo used to derive the second
// from the first by appending a TLD, which only worked because that TLD was one
// it resolved itself. On a real server there is nothing to append.

// domainLabel matches one DNS label: alphanumeric, inner hyphens allowed, 63
// characters at most.
var domainLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// SiteName derives the internal handle from a directory name. It strips one
// trailing TLD (a gTLD from the curated list or any 2-letter ccTLD) then folds
// remaining dots to dashes, so a directory named for its domain still yields a
// single label:
//
//	"myapp"              -> "myapp"
//	"myapp.com"          -> "myapp"
//	"admin.astrolov.com" -> "admin-astrolov"
func SiteName(dirName string) string {
	name := strings.ToLower(dirName)
	if stripped, ok := stripGTLD(name); ok {
		name = stripped
	} else if m := ccTLDPattern.FindStringIndex(name); m != nil {
		name = name[:m[0]]
	}
	name = strings.ReplaceAll(name, ".", "-")
	// Strip characters that would be unsafe in a systemd unit name or body
	// derived from this handle (newline and NUL inject a directive, a slash
	// escapes the path).
	name = unsafeNameChars.ReplaceAllString(name, "")
	if name == "" {
		name = "site"
	}
	return name
}

// NormalizeDomain validates a domain the operator supplied and returns its
// canonical form: lower-cased, with any scheme, path, trailing dot or
// surrounding space removed.
//
// It requires a fully qualified name. A bare label is refused rather than
// completed, because completing it is exactly what this replaces: "myapp"
// silently became "myapp.test", which resolved only on the machine that made it
// up. An IP address is refused too: a certificate cannot be issued for one, and
// a vhost keyed on an address serves every name that reaches it.
func NormalizeDomain(raw string) (string, error) {
	d := strings.TrimSpace(strings.ToLower(raw))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	d = strings.TrimSuffix(d, "/")
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	d = strings.TrimSuffix(d, ".")

	if d == "" {
		return "", fmt.Errorf("no domain given: a site needs a fully qualified domain, e.g. example.com")
	}
	if net.ParseIP(d) != nil {
		return "", fmt.Errorf("%q is an IP address: a site needs a domain, e.g. example.com", raw)
	}
	if !strings.Contains(d, ".") {
		return "", fmt.Errorf("%q is not a fully qualified domain: give the whole name, e.g. %s.example.com", raw, d)
	}
	for _, label := range strings.Split(d, ".") {
		if !domainLabel.MatchString(label) {
			return "", fmt.Errorf("%q is not a valid domain: %q is not a usable label", raw, label)
		}
	}
	return d, nil
}

// DomainFromDirName offers a directory name as a domain when it already is one,
// which is the ordinary case on a server where a project lives in a directory
// named for the site it serves. Returns "" when the name is not a domain, and
// the caller must then ask.
func DomainFromDirName(dirName string) string {
	d, err := NormalizeDomain(dirName)
	if err != nil {
		return ""
	}
	return d
}
