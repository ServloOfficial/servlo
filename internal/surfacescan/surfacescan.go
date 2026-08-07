// Package surfacescan is the standing gate that keeps deleted features
// deleted. PRD §4 removes a set of upstream features outright rather than
// hiding them behind a flag, so the only durable check is one that reads the
// tree and fails when a name comes back.
//
// A rule that is not Enforced names a feature a later Phase 0 story still has
// to delete. Those are reported as pending rather than failed, so the gate is
// green from the day it lands and each story turns its own rule on.
package surfacescan

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Rule is one deleted feature and the patterns that would betray its return.
// Patterns are matched against both a file's path and each of its lines.
type Rule struct {
	Feature  string
	Story    string
	Enforced bool
	Patterns []string
	// Allow lists path substrings the rule tolerates: the retained upstream
	// references, and the specification documents that describe the deletions.
	Allow []string
	// Only, when set, limits the rule to paths carrying one of these
	// substrings. For a rule whose forbidden string legitimately appears
	// elsewhere, scoping is more honest than an allowlist that grows a line
	// every time someone touches an unrelated file.
	Only []string
}

// Finding is one place a rule matched. Line is 0 for a path-level match.
type Finding struct {
	Rule  string
	Story string
	Path  string
	Line  int
	Text  string
}

// skipDirs are generated, vendored or version-control trees. Scanning them
// says nothing about the source and would drown a real finding.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	".svelte-kit":  true,
	// paraglide is generated from messages/*.json, which is scanned instead.
	"paraglide": true,
}

// textExts are the file kinds a deleted feature can hide in. Anything else is
// a binary or an asset and is checked by name only.
var textExts = map[string]bool{
	".go": true, ".ts": true, ".js": true, ".mjs": true, ".svelte": true,
	".vue": true, ".css": true, ".json": true, ".yml": true, ".yaml": true,
	".sh": true, ".md": true, ".py": true, ".service": true, ".container": true,
	".ini": true, ".conf": true, ".html": true, ".bats": true,
}

// maxFileSize skips files too large to be hand-written source. Reading a
// multi-megabyte lockfile line by line costs more than it can ever find.
const maxFileSize = 2 << 20

type compiled struct {
	rule Rule
	res  []*regexp.Regexp
}

func compile(r Rule) (compiled, error) {
	c := compiled{rule: r}
	for _, p := range r.Patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return compiled{}, fmt.Errorf("pattern %q: %w", p, err)
		}
		c.res = append(c.res, re)
	}
	return c, nil
}

func (c compiled) allows(path string) bool {
	// A scoped rule ignores everything outside its scope, which is how a rule
	// whose forbidden string legitimately appears elsewhere stays strict where
	// it matters without an allowlist that grows on every unrelated change.
	if len(c.rule.Only) > 0 {
		inScope := false
		for _, o := range c.rule.Only {
			if strings.Contains(path, o) {
				inScope = true
				break
			}
		}
		if !inScope {
			return true
		}
	}
	for _, a := range c.rule.Allow {
		if strings.Contains(path, a) {
			return true
		}
	}
	return false
}

func (c compiled) match(s string) bool {
	split := splitCamel(s)
	for _, re := range c.res {
		if re.MatchString(s) || re.MatchString(split) {
			return true
		}
	}
	return false
}

var (
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymTail   = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

// splitCamel spaces out camel-case identifiers so a rule can be written as a
// plain word: startTray and MCPInject become "start Tray" and "MCP Inject",
// which \btray\b and \bmcp\b then see. Without this a deleted feature comes
// back simply by living inside an identifier.
func splitCamel(s string) string {
	s = acronymTail.ReplaceAllString(s, "$1 $2")
	return camelBoundary.ReplaceAllString(s, "$1 $2")
}

// ScanSource walks root and returns a finding for every enforced rule that
// matches. Pending rules are compiled and validated but never reported.
func ScanSource(root string, rules []Rule) ([]Finding, error) {
	var active []compiled
	for _, r := range rules {
		c, err := compile(r)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.Feature, err)
		}
		if r.Enforced {
			active = append(active, c)
		}
	}
	if len(active) == 0 {
		return nil, nil
	}

	var found []Finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if rel != "." && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		for _, c := range active {
			if c.allows(rel) {
				continue
			}
			if c.match(rel) {
				found = append(found, Finding{Rule: c.rule.Feature, Story: c.rule.Story, Path: rel})
			}
		}
		if !textExts[filepath.Ext(path)] {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || info.Size() > maxFileSize {
			return nil
		}
		lines, readErr := scanLines(path, rel, active)
		if readErr != nil {
			return readErr
		}
		found = append(found, lines...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func scanLines(path, rel string, active []compiled) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var found []Finding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		for _, c := range active {
			if c.allows(rel) || !c.match(line) {
				continue
			}
			found = append(found, Finding{
				Rule: c.rule.Feature, Story: c.rule.Story,
				Path: rel, Line: n, Text: strings.TrimSpace(line),
			})
		}
	}
	// A file with a line longer than the buffer is almost certainly minified
	// output that slipped past skipDirs; the name check above already covered it.
	if err := sc.Err(); err != nil && !strings.Contains(err.Error(), "token too long") {
		return nil, err
	}
	return found, nil
}
