// Package fpmpool renders a PHP-FPM pool for one site.
//
// Servlo used to run one FPM container per PHP version and give every site on
// that version the same php.ini, so raising an upload limit for one site raised
// it for all of them and a site could not have its own memory limit at all.
//
// A pool is FPM's own answer to that, and it is what this writes. One master
// process per version still, because a container per site would be twenty
// containers on a twenty-site droplet; but a pool inside it per site, each
// listening on its own unix socket and carrying its own settings. nginx sends a
// site's requests to that site's socket, and the settings that apply are the
// ones written here.
//
// Everything a pool interpolates comes from the site registry, which a person
// edits, so a value that would end a directive and begin another one is refused
// rather than escaped.
package fpmpool

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Settings is one site's pool.
type Settings struct {
	// Site is the site handle: the pool's name and its socket's filename.
	Site string
	// Root is the site directory, which the pool chdirs into.
	Root string
	// SocketDir is where the pool's socket lives, a directory both the FPM
	// container and the nginx container can see.
	SocketDir string

	// MaxUploadMB writes upload_max_filesize and post_max_size together. Zero
	// leaves both to the production defaults.
	//
	// One field for two directives because they are one decision: raising only
	// upload_max_filesize leaves the larger upload refused by post_max_size,
	// which reads to an operator as the setting not working.
	MaxUploadMB int
	// MaxExecutionSeconds writes max_execution_time. Its nginx half, the
	// fastcgi read and send timeouts, is written by the vhost from the same
	// field. Zero leaves the default.
	MaxExecutionSeconds int
	// MemoryLimitMB writes memory_limit. Zero leaves the default.
	MemoryLimitMB int
}

// siteHandle is a single safe token: it names a pool, a socket file and a
// systemd-adjacent path, so it may not name a path or carry whitespace.
var siteHandle = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Bounds. A value outside them produces a pool FPM refuses to start with, which
// takes down every site sharing the container, so they are refused here.
const (
	maxUploadCeilingMB    = 16384
	maxExecutionCeilingS  = 86400
	maxMemoryLimitCeiling = 65536
)

// Render returns the pool configuration for a site.
func Render(s Settings) (string, error) {
	if !siteHandle.MatchString(s.Site) {
		return "", fmt.Errorf("%q is not a usable site handle for a pool", s.Site)
	}
	if err := safeDirective(s.Root, "the site root"); err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.Root) {
		return "", fmt.Errorf("the site root %q is not an absolute path", s.Root)
	}
	if err := safeDirective(s.SocketDir, "the socket directory"); err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.SocketDir) {
		return "", fmt.Errorf("the socket directory %q is not an absolute path", s.SocketDir)
	}
	if err := s.checkRanges(); err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "; Written by servlo for the site %s. Edited by hand, this is\n", s.Site)
	fmt.Fprintf(&b, "; overwritten the next time the site's settings change.\n")
	fmt.Fprintf(&b, "[%s]\n", s.Site)
	fmt.Fprintf(&b, "listen = %s\n", SocketPath(s.SocketDir, s.Site))
	// 0660 with the group nginx runs as: both containers run as the same user
	// here, and a socket any process on the box can reach is a PHP executor any
	// process on the box can reach.
	fmt.Fprintf(&b, "listen.mode = 0660\n")
	fmt.Fprintf(&b, "chdir = %s\n", s.Root)
	fmt.Fprintf(&b, "pm = dynamic\n")
	fmt.Fprintf(&b, "pm.max_children = 10\n")
	fmt.Fprintf(&b, "pm.start_servers = 2\n")
	fmt.Fprintf(&b, "pm.min_spare_servers = 1\n")
	fmt.Fprintf(&b, "pm.max_spare_servers = 3\n")

	// Production settings the site does not get to turn off. admin_value
	// rather than value, so an application's own ini_set cannot put a stack
	// trace in front of a visitor.
	fmt.Fprintf(&b, "php_admin_value[display_errors] = Off\n")
	fmt.Fprintf(&b, "php_admin_value[expose_php] = Off\n")

	// Only what the operator actually set. Restating a default here would mean
	// the shared production ini could no longer move it.
	if s.MaxUploadMB > 0 {
		fmt.Fprintf(&b, "php_admin_value[upload_max_filesize] = %dM\n", s.MaxUploadMB)
		fmt.Fprintf(&b, "php_admin_value[post_max_size] = %dM\n", s.MaxUploadMB)
	}
	if s.MaxExecutionSeconds > 0 {
		fmt.Fprintf(&b, "php_admin_value[max_execution_time] = %d\n", s.MaxExecutionSeconds)
	}
	if s.MemoryLimitMB > 0 {
		fmt.Fprintf(&b, "php_admin_value[memory_limit] = %dM\n", s.MemoryLimitMB)
	}
	return b.String(), nil
}

func (s Settings) checkRanges() error {
	switch {
	case s.MaxUploadMB < 0 || s.MaxUploadMB > maxUploadCeilingMB:
		return fmt.Errorf("the max upload size %dM is outside 0 to %dM", s.MaxUploadMB, maxUploadCeilingMB)
	case s.MaxExecutionSeconds < 0 || s.MaxExecutionSeconds > maxExecutionCeilingS:
		return fmt.Errorf("the max execution time %ds is outside 0 to %ds", s.MaxExecutionSeconds, maxExecutionCeilingS)
	case s.MemoryLimitMB < 0 || s.MemoryLimitMB > maxMemoryLimitCeiling:
		return fmt.Errorf("the memory limit %dM is outside 0 to %dM", s.MemoryLimitMB, maxMemoryLimitCeiling)
	}
	return nil
}

// safeDirective refuses a value that would not stay on its own line. A newline
// in a path ends the directive it sits in and starts one the caller did not
// write.
func safeDirective(v, what string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s is empty", what)
	}
	if strings.ContainsAny(v, "\n\r\x00") {
		return fmt.Errorf("%s contains a newline, which would end the directive it is written into", what)
	}
	return nil
}

// SocketPath is where a site's pool listens. Exported because the vhost has to
// name the same path, and two places computing it separately is how they drift.
func SocketPath(socketDir, site string) string {
	return filepath.Join(socketDir, site+".sock")
}

// Path is the pool file for a site inside dir.
func Path(dir, site string) string {
	return filepath.Join(dir, site+".conf")
}

// Write renders the pool and puts it where FPM reads pools from, replacing any
// previous one. Replacing rather than appending matters: two pools with the
// same name is a configuration FPM refuses to start with, which would take down
// every site on that version.
func Write(dir string, s Settings) (string, error) {
	body, err := Render(s)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := Path(dir, s.Site)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Remove takes a site's pool away. An absent pool is not an error: a site that
// never had one is the ordinary case on an install that predates per-site
// pools.
func Remove(dir, site string) error {
	if !siteHandle.MatchString(site) {
		return fmt.Errorf("%q is not a usable site handle for a pool", site)
	}
	if err := os.Remove(Path(dir, site)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
