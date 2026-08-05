package git

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MainBranch returns the current branch of the repo checkout at sitePath, or an
// empty string if it cannot be determined. Read straight from .git/HEAD rather
// than shelling out, since this is called once per site on every snapshot.
func MainBranch(sitePath string) string {
	data, err := os.ReadFile(filepath.Join(sitePath, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix)
	}
	if len(line) >= 7 {
		return "detached-" + line[:7]
	}
	return ""
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9-]`)
var multiHyphen = regexp.MustCompile(`-{2,}`)

// SanitizeBranch converts a branch name to a subdomain-safe slug. Used to
// validate the group-secondary labels that share the same host-label rules.
func SanitizeBranch(branch string) string {
	s := strings.ToLower(branch)
	s = strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(s)
	s = nonSlugChars.ReplaceAllString(s, "")
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.TrimRight(s[:50], "-")
	}
	if s == "" {
		return "branch"
	}
	return s
}
