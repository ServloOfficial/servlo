package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	gitpkg "github.com/realrashid/servlo/internal/git"
)

// The exclude list, and the one thing it has to stop.
//
// Most of what these directories hold is invisible to git. On an ordinary
// WordPress install `wp-content/uploads` and `wp-content/plugins` are ignored,
// so they are untracked, and an untracked file is one a pull cannot touch. The
// list has nothing to do on those sites, and that is the common case.
//
// The sites that lose a client's data are the ones whose repository committed
// those directories, and there git behaves in two different ways. A file the
// operator changed locally, git refuses to overwrite: the pull fails, loudly,
// saying which file. A file that was deleted upstream, git deletes here,
// silently, because from git's point of view nothing local was at risk. That
// second case is the whole gap. A developer tidying a repository from a
// checkout that never had the client's plugins removes them from every site on
// it, and the operator finds out from the client.
//
// So the list protects against deletion, which is also what makes the repair
// clean: the file is gone from the new commit, so writing the old content back
// leaves an untracked file rather than a modified tracked one, and every later
// pull is as clean as it was before. Restoring over a file git still tracks
// would leave the tree permanently dirty and wedge the next deploy, which is a
// worse failure than the one being prevented because it arrives later.

// Keep restores files under the excluded paths that the update deleted, and
// returns the ones it kept.
func Keep(dir, from, to string, excludes []string, out writer) ([]string, error) {
	if len(excludes) == 0 || from == "" || from == to {
		return nil, nil
	}

	gone, err := deletedUnder(dir, from, to, excludes)
	if err != nil {
		return nil, err
	}
	if len(gone) == 0 {
		return nil, nil
	}

	for _, rel := range gone {
		if err := restoreFile(dir, from, rel); err != nil {
			return nil, err
		}
	}

	fmt.Fprintf(out, "\nKept %s the update removed, under %s.\n",
		plural(len(gone), "file"), strings.Join(excludes, ", "))
	for _, rel := range gone {
		fmt.Fprintf(out, "  %s\n", rel)
	}
	return gone, nil
}

// deletedUnder asks git which files under the excluded paths the update
// removed.
//
// Asked of git rather than worked out by walking the tree before and after: a
// site's uploads directory is every image a client has ever added, and copying
// it aside on every deploy to find out that nothing changed would make the
// protection cost more than the thing it protects.
func deletedUnder(dir, from, to string, excludes []string) ([]string, error) {
	var paths []string
	for _, p := range excludes {
		if p = path.Clean(strings.TrimSpace(p)); p != "" && p != "." {
			// A literal path, so a list entry containing a glob character
			// protects what it spells rather than what git reads it as.
			paths = append(paths, ":(literal)"+p)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	args := append([]string{"diff", "--name-only", "-z", "--diff-filter=D", from + ".." + to, "--"}, paths...)

	out, err := gitpkg.Output(dir, args...)
	if err != nil {
		return nil, fmt.Errorf("asking git what the update removed: %w", err)
	}

	var gone []string
	for _, rel := range strings.Split(out, "\x00") {
		if rel != "" {
			gone = append(gone, rel)
		}
	}
	return gone, nil
}

// restoreFile writes a file back at the content it had before the update.
//
// Through `git show` to a new file rather than `git checkout -- <path>`: a
// checkout stages the path, which would leave the site carrying a permanent
// staged change and turn every later deploy into a conflict. Writing the bytes
// leaves the index alone, so the file lands untracked, which is what it now is.
func restoreFile(dir, from, rel string) error {
	if err := safeRelPath(rel); err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("keeping %s: %w", rel, err)
	}

	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("keeping %s: %w", rel, err)
	}
	defer f.Close()

	// Straight to the file, so an upload is the bytes it was rather than
	// something that went through a string.
	cmd := exec.Command("git", "show", from+":"+rel)
	cmd.Dir = dir
	cmd.Stdout = f
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keeping %s: reading it back from %s: %w", rel, short(from), err)
	}
	return f.Close()
}

// safeRelPath refuses a path that would land outside the site. git does not
// produce one, but this is the step that writes files from a name, and the cost
// of being sure is a comparison.
func safeRelPath(rel string) error {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return fmt.Errorf("git named an unusable path %q", rel)
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return fmt.Errorf("git named an absolute path %q", rel)
	}
	if cleaned := path.Clean(rel); cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("git named a path outside the site %q", rel)
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
