// Package logrotate keeps application logs from filling the disk.
//
// The division here follows who owns the file. nginx, PHP-FPM and the workers
// all run under systemd, so their output is in the journal, and the journal's
// size is journald's business: one system setting, needing root, printed for a
// person to run rather than fiddled with here (CLAUDE.md §3.2). What servlo
// owns is the other half, the application's own log files inside each site,
// declared by the framework as log sources. Nothing else rotates those, and a
// single Laravel log growing for eleven months is one of the two commonest ways
// a droplet's disk disappears.
//
// Which files those are is store data, not knowledge in Go. A framework
// declares them under logs: and servlo rotates whatever the glob matches.
package logrotate

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Policy is how much log a site is allowed to keep.
type Policy struct {
	// MaxSizeMB is how large a log gets before it is rotated. Zero uses the
	// default.
	MaxSizeMB int `yaml:"max_size_mb" json:"max_size_mb"`
	// Keep is how many rotated copies survive, oldest deleted first.
	Keep int `yaml:"keep" json:"keep"`
	// Compress gzips a rotation as it is made. Log files are text and
	// compress to roughly a tenth, which is the difference between keeping a
	// week of them and not.
	Compress bool `yaml:"compress" json:"compress"`
}

// DefaultPolicy is what a server that has said nothing gets.
//
// Rotating at 50 MB and keeping five means a site's logs are bounded at roughly
// 300 MB uncompressed, or 80 MB with compression on, which is the default. That
// is generous enough that nobody loses a log they were reading and small enough
// that it cannot be the thing that fills a 25 GB droplet.
var DefaultPolicy = Policy{MaxSizeMB: 50, Keep: 5, Compress: true}

// Resolved fills in anything the operator left at zero, so a partially set
// policy cannot turn into "rotate everything at 0 bytes and keep none".
func (p Policy) Resolved() Policy {
	if p.MaxSizeMB <= 0 {
		p.MaxSizeMB = DefaultPolicy.MaxSizeMB
	}
	if p.Keep <= 0 {
		p.Keep = DefaultPolicy.Keep
	}
	return p
}

// Result is what one pass did.
type Result struct {
	// Rotated names the live logs that were rotated out.
	Rotated []string
	// Removed counts copies deleted for being past the keep count.
	Removed int
	// Freed is how many bytes stopped being live log. Compression means the
	// disk saving is larger than this.
	Freed int64
}

// rotatedSuffix matches a copy this package made: name.1, name.2.gz. Anchored
// on the whole tail so an application's own dated file, api-2026-08-09.log, is
// never mistaken for one and swept.
func rotatedName(base string, n int, compress bool) string {
	name := base + "." + strconv.Itoa(n)
	if compress {
		name += ".gz"
	}
	return name
}

// Rotate rotates every log matching one of the globs, relative to root.
//
// The globs are the framework's, so servlo needs to know nothing about where
// any particular framework keeps its logs.
func Rotate(root string, globs []string, p Policy) (Result, error) {
	p = p.Resolved()
	limit := int64(p.MaxSizeMB) << 20

	var res Result
	seen := map[string]bool{}
	for _, glob := range globs {
		matches, err := filepath.Glob(filepath.Join(root, glob))
		if err != nil {
			// A glob the definition got wrong is worth saying out loud rather
			// than silently rotating nothing for that framework forever.
			return res, fmt.Errorf("log path %q is not a usable pattern: %w", glob, err)
		}
		sort.Strings(matches)
		for _, path := range matches {
			if seen[path] || isRotated(path) {
				continue
			}
			seen[path] = true
			// Lstat, not Stat. A symlink named like a log is a way to have a
			// rotation copy something from outside the site into it, and
			// measuring the link rather than what it points at is what makes it
			// too small to rotate and so never followed.
			info, err := os.Lstat(path)
			if err != nil || info.Size() < limit {
				continue
			}
			if err := rotateOne(path, p); err != nil {
				return res, err
			}
			res.Rotated = append(res.Rotated, path)
			res.Freed += info.Size()
		}
	}

	for path := range seen {
		removed, err := prune(path, p)
		if err != nil {
			return res, err
		}
		res.Removed += removed
	}
	return res, nil
}

// rotateOne shifts the existing copies up and moves the live log into slot 1.
//
// A rename, not a copy and truncate. Every application log servlo sees is
// written by PHP, whose handlers open and close around a request, so the next
// write lands in a fresh file. A copy would also need twice the log's size in
// free disk at exactly the moment a disk is filling up, which is when this runs.
func rotateOne(path string, p Policy) error {
	for n := p.Keep - 1; n >= 1; n-- {
		from := rotatedName(path, n, p.Compress)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := os.Rename(from, rotatedName(path, n+1, p.Compress)); err != nil {
			return err
		}
	}
	if !p.Compress {
		return os.Rename(path, rotatedName(path, 1, false))
	}
	if err := compress(path, rotatedName(path, 1, true)); err != nil {
		return err
	}
	// Removed only once the compressed copy is complete, so an interrupted
	// rotation loses nothing.
	return os.Remove(path)
}

func compress(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck

	// 0600, the same as the log it came from: an application log holds request
	// paths, and often more than the person who wrote the log line intended.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		_ = zw.Close()
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

// prune deletes the copies past the keep count.
func prune(path string, p Policy) (int, error) {
	var removed int
	for n := p.Keep + 1; n <= p.Keep+50; n++ {
		for _, name := range []string{rotatedName(path, n, true), rotatedName(path, n, false)} {
			if _, err := os.Stat(name); err != nil {
				continue
			}
			if err := os.Remove(name); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

// isRotated reports a file this package produced, so a second pass does not
// rotate its own output into a chain of .1.1.1.
func isRotated(path string) bool {
	name := strings.TrimSuffix(path, ".gz")
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return false
	}
	n, err := strconv.Atoi(name[i+1:])
	return err == nil && n > 0
}
