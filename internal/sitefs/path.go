// Package sitefs is the path jail the file manager works through.
//
// Every operation the panel offers over a site's files - listing, reading,
// saving, uploading, extracting, deleting, fixing modes - takes a path that
// arrived in an HTTP request, and a path that arrived in an HTTP request is the
// entire security surface of this feature. So the jail is one package with one
// job: turn a caller's relative path into an absolute one that is provably
// inside the site root, or refuse.
//
// What "inside" means here is worth being precise about, because it is easy to
// implement three quarters of it. It means inside after the whole path has been
// resolved, symlinks included, and it means the same for a path whose last
// component does not exist yet. A check that cleans the string and compares
// prefixes catches `../` and nothing else; a symlink named "escape" pointing at
// /etc walks straight past it.
//
// What this does not do, and cannot: it is not a privilege boundary. Every site
// on a servlo machine runs as the same Linux user (PRD section 6), so a bug in
// this file is not a bug that leaks one site into another, it is a bug that
// leaks the whole account. That is why the jail is a package with its own tests
// rather than a helper function beside a handler.
package sitefs

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot is the refusal every escape ends in. One error for every
// shape of escape, because the caller's answer is the same in each case and a
// message that distinguishes them is a message that describes the filesystem to
// somebody who is trying to map it.
var ErrOutsideRoot = errors.New("that path is outside the site directory")

// Root is a site directory, resolved once so every later comparison is between
// two real paths.
type Root struct {
	path string
}

// Open resolves a site directory into a Root.
func Open(sitePath string) (Root, error) {
	if sitePath == "" {
		return Root{}, fmt.Errorf("no site directory to open")
	}
	resolved, err := filepath.EvalSymlinks(sitePath)
	if err != nil {
		return Root{}, fmt.Errorf("reading the site directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Root{}, fmt.Errorf("reading the site directory: %w", err)
	}
	if !info.IsDir() {
		return Root{}, fmt.Errorf("%s is not a directory", sitePath)
	}
	return Root{path: resolved}, nil
}

// Path is the resolved site root.
func (r Root) Path() string { return r.path }

// Rel is the path of an absolute location relative to the root, in slash form,
// for reporting one back to the panel. It assumes the caller got the path from
// this package and so does not re-check containment.
func (r Root) Rel(abs string) string {
	rel, err := filepath.Rel(r.path, abs)
	if err != nil {
		return ""
	}
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// ResolveExisting returns the absolute path of something that is already there,
// refusing anything that resolves outside the root.
func (r Root) ResolveExisting(rel string) (string, error) {
	lexical, err := r.lexical(rel)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(lexical)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", displayName(rel), err)
	}
	if !r.contains(resolved) {
		return "", ErrOutsideRoot
	}
	return resolved, nil
}

// ResolveNew returns the absolute path of something that may not exist yet: an
// upload's destination, a file being created by an extraction.
//
// The final component is the only one allowed to be missing. Its parent is
// resolved in full, which is what catches a directory symlink that leaves the
// root with a name behind it that nobody has created yet.
func (r Root) ResolveNew(rel string) (string, error) {
	lexical, err := r.lexical(rel)
	if err != nil {
		return "", err
	}
	return r.resolveDeepest(lexical, rel)
}

// resolveDeepest resolves the deepest ancestor that exists, checks that it is
// inside the root, and re-attaches the components that are not there yet.
//
// Walking up rather than stopping at the immediate parent is what makes
// extraction work: an archive creates whole directory trees, so the parent of
// the first file it writes does not exist either. The components re-attached
// afterwards cannot themselves be symlinks, because they do not exist.
func (r Root) resolveDeepest(lexical, rel string) (string, error) {
	var missing []string
	current := lexical
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if !r.contains(resolved) {
				return "", ErrOutsideRoot
			}
			target := resolved
			for i := len(missing) - 1; i >= 0; i-- {
				target = filepath.Join(target, missing[i])
			}
			if !r.contains(target) {
				return "", ErrOutsideRoot
			}
			return target, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("reading %s: %w", displayName(rel), err)
		}
		parent, base := filepath.Split(current)
		parent = filepath.Clean(parent)
		if base == "" || parent == current {
			return "", ErrOutsideRoot
		}
		missing = append(missing, base)
		current = parent
		// The root itself always exists, so climbing past it means the path was
		// never inside it and the loop has nothing left to find.
		if !r.contains(current) {
			return "", ErrOutsideRoot
		}
	}
}

// ResolveLeaf resolves everything above the last component and leaves that one
// alone, for an operation that acts on a name rather than through it. Deleting
// a symlink is the case: following it would remove what it points at and leave
// the link behind, which is the opposite of what was asked for.
func (r Root) ResolveLeaf(rel string) (string, error) {
	lexical, err := r.lexical(rel)
	if err != nil {
		return "", err
	}
	if lexical == r.path {
		return "", fmt.Errorf("that operation needs a path inside the site directory")
	}
	parent, base := filepath.Split(lexical)
	if base == "" {
		return "", ErrOutsideRoot
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Clean(parent))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", displayName(path.Dir(rel)), err)
	}
	if !r.contains(resolvedParent) {
		return "", ErrOutsideRoot
	}
	target := filepath.Join(resolvedParent, base)
	if !r.contains(target) {
		return "", ErrOutsideRoot
	}
	return target, nil
}

// lexical does the string half: refuse the shapes that are wrong before the
// filesystem is touched at all, then join.
//
// The `..` refusal is a refusal rather than a rewrite on purpose. Cleaning
// "app/../.." against a leading slash turns it into the root, which is a
// perfectly valid path that is not the one the caller asked for. Silently
// answering a different question is how a jail ends up reporting success on an
// escape attempt.
func (r Root) lexical(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("that path contains a NUL byte")
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return r.path, nil
	}
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: paths are relative to the site directory", ErrOutsideRoot)
	}
	cleaned := path.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrOutsideRoot
	}
	if cleaned == "." {
		return r.path, nil
	}
	joined := filepath.Join(r.path, filepath.FromSlash(cleaned))
	if !r.contains(joined) {
		return "", ErrOutsideRoot
	}
	return joined, nil
}

// contains reports whether an already-resolved absolute path is the root or
// beneath it. The separator matters: without it "/srv/site-backup" reads as a
// child of "/srv/site".
func (r Root) contains(abs string) bool {
	if abs == r.path {
		return true
	}
	return strings.HasPrefix(abs, r.path+string(os.PathSeparator))
}

// displayName is what an error calls a path. The site-relative spelling, never
// the absolute one: an error message is a place a filesystem layout leaks.
func displayName(rel string) string {
	if rel == "" || rel == "." {
		return "the site directory"
	}
	return rel
}
