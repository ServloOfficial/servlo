package sitefs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// The operations themselves. Every one of them starts by putting its path
// through the jail in path.go and then works with the absolute path that came
// back, so there is exactly one place that decides what "inside the site" means.

// maxListEntries bounds one directory listing. A vendor directory with sixty
// thousand entries is not a page anybody reads, and rendering it is how the
// panel becomes the slowest thing on the machine.
const maxListEntries = 2000

// Entry is one thing in a directory.
type Entry struct {
	Name string `json:"name"`
	// Path is site-relative and in slash form, so the panel can hand it
	// straight back as the next request's path.
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Mode     string `json:"mode"`
	Modified int64  `json:"modified"`
	Symlink  bool   `json:"symlink"`
	// Escapes marks a symlink whose target is outside the site. It is listed
	// and never followed: hiding it would leave an operator wondering where a
	// file went, and following it would be the escape itself.
	Escapes bool `json:"escapes,omitempty"`
}

// Listing is one directory.
type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
	// Truncated says the directory holds more than servlo will render.
	Truncated bool `json:"truncated"`
}

// List reads one directory inside the site.
func (r Root) List(rel string) (Listing, error) {
	dir, err := r.ResolveExisting(rel)
	if err != nil {
		return Listing{}, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Listing{}, err
	}
	if !info.IsDir() {
		return Listing{}, fmt.Errorf("%s is a file, not a directory", displayName(rel))
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		return Listing{}, err
	}

	listing := Listing{Path: r.Rel(dir)}
	if len(names) > maxListEntries {
		names = names[:maxListEntries]
		listing.Truncated = true
	}
	for _, d := range names {
		listing.Entries = append(listing.Entries, r.describe(dir, d.Name()))
	}
	// Directories first, then by name. The alternative is an operator hunting
	// for a folder in the middle of four hundred files.
	sort.SliceStable(listing.Entries, func(i, j int) bool {
		a, b := listing.Entries[i], listing.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return listing, nil
}

// describe reads one entry without following it. Lstat rather than Stat is the
// whole point: a symlink is reported as a symlink, and where it points is
// answered by the jail rather than by chasing it.
func (r Root) describe(dir, name string) Entry {
	full := filepath.Join(dir, name)
	entry := Entry{Name: name, Path: r.Rel(full)}
	info, err := os.Lstat(full)
	if err != nil {
		return entry
	}
	entry.Mode = fmt.Sprintf("%04o", info.Mode().Perm())
	entry.Modified = info.ModTime().Unix()
	if info.Mode()&os.ModeSymlink != 0 {
		entry.Symlink = true
		if _, err := r.ResolveExisting(entry.Path); err != nil {
			entry.Escapes = true
			return entry
		}
		if target, err := os.Stat(full); err == nil {
			entry.Dir = target.IsDir()
			entry.Size = target.Size()
		}
		return entry
	}
	entry.Dir = info.IsDir()
	if !entry.Dir {
		entry.Size = info.Size()
	}
	return entry
}

// Content is a file the panel is about to show in an editor.
type Content struct {
	Path string `json:"path"`
	Text string `json:"text"`
	Size int64  `json:"size"`
	Mode string `json:"mode"`
	// Binary means servlo will not put this in a text editor. Saving what a
	// textarea made of an image is how an image stops being one.
	Binary bool `json:"binary"`
	// Truncated means the file is longer than the editor was given.
	Truncated bool `json:"truncated"`
}

// Read loads a file for editing, up to max bytes.
func (r Root) Read(rel string, max int64) (Content, error) {
	target, err := r.ResolveExisting(rel)
	if err != nil {
		return Content{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return Content{}, err
	}
	if info.IsDir() {
		return Content{}, fmt.Errorf("%s is a directory", displayName(rel))
	}
	f, err := os.Open(target)
	if err != nil {
		return Content{}, err
	}
	defer f.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return Content{}, err
	}
	content := Content{
		Path:      r.Rel(target),
		Size:      info.Size(),
		Mode:      fmt.Sprintf("%04o", info.Mode().Perm()),
		Truncated: info.Size() > int64(len(data)),
	}
	if looksBinary(data) {
		content.Binary = true
		return content, nil
	}
	content.Text = string(data)
	return content, nil
}

// looksBinary is the same rule git uses, and for the same reason: a NUL byte in
// the first few kilobytes means nobody wants this in a text editor.
func looksBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if len(head) == 0 {
		return false
	}
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return !utf8.Valid(head)
}

// Write saves a file, atomically, so a failed write leaves the previous
// contents rather than half of the new ones on a live site.
func (r Root) Write(rel string, data []byte) error {
	target, err := r.ResolveNew(rel)
	if err != nil {
		return err
	}
	mode := modeFor(rel, fileMode)
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", displayName(rel))
		}
		// An existing file keeps the mode it had, except that a secret is
		// pulled back to owner-only whatever it was before.
		mode = info.Mode().Perm()
		if secret := modeFor(rel, 0); secret != 0 {
			mode = secret
		}
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".servlo-save-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// Delete removes one file, link or directory tree.
func (r Root) Delete(rel string) error {
	// ResolveLeaf rather than ResolveExisting, so deleting a link removes the
	// link. It also refuses the root, which is the one path in the tree that
	// this must never be able to remove.
	target, err := r.ResolveLeaf(rel)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("reading %s: %w", displayName(rel), err)
	}
	if info.IsDir() {
		return os.RemoveAll(target)
	}
	return os.Remove(target)
}

// Upload writes one uploaded file into a directory inside the site.
//
// The name is taken apart rather than trusted: a browser sends whatever the
// form said, and a form can say "../../.ssh/authorized_keys".
func (r Root) Upload(dirRel, name string, src io.Reader, max int64) (int64, error) {
	if err := safeFileName(name); err != nil {
		return 0, err
	}
	dir, err := r.ResolveExisting(dirRel)
	if err != nil {
		return 0, err
	}
	if info, err := os.Stat(dir); err != nil {
		return 0, err
	} else if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", displayName(dirRel))
	}
	target, err := r.ResolveNew(path.Join(r.Rel(dir), name))
	if err != nil {
		return 0, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".servlo-upload-")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck

	// max+1 so a file of exactly the ceiling is accepted and the one byte over
	// it is refused, rather than the LimitReader quietly truncating.
	written, err := io.Copy(tmp, io.LimitReader(src, max+1))
	if err != nil {
		tmp.Close() //nolint:errcheck
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if written > max {
		return 0, fmt.Errorf("that file is larger than the %s servlo will accept", humanBytes(max))
	}
	if err := os.Chmod(tmp.Name(), modeFor(name, fileMode)); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return 0, err
	}
	return written, nil
}

// safeFileName refuses anything that is a path rather than a name.
func safeFileName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("that upload has no usable filename")
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("that filename contains a NUL byte")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("a filename may not contain a path separator")
	}
	return nil
}

// humanBytes is for a refusal an operator reads, not for a table.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%cB", float64(n)/float64(div), "KMGT"[exp])
}

// modeFor is the mode a newly written file gets. Secrets are owner-only,
// everything else is the ordinary file mode; fallback is what a non-secret gets
// and may be zero when the caller only wants to know whether this is a secret.
func modeFor(name string, fallback fs.FileMode) fs.FileMode {
	if isSecretName(path.Base(filepath.ToSlash(name))) {
		return secretMode
	}
	return fallback
}
