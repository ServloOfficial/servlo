package surfacescan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes files under a temp root and returns the root.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// startTray is camel-cased on purpose: a word-boundary match alone would miss
// it, which is the failure mode splitCamel exists to close.
func TestScanSource_ReportsAnEnforcedRuleThatReappears(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/cli/thing.go": "package cli\n\nfunc run() { startTray() }\n",
	})
	rules := []Rule{{Feature: "system tray", Story: "S0.2", Enforced: true, Patterns: []string{`(?i)\btray\b`}}}

	found, err := ScanSource(root, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(found), found)
	}
	if found[0].Line != 3 || found[0].Path != "internal/cli/thing.go" {
		t.Errorf("finding = %+v, want internal/cli/thing.go:3", found[0])
	}
}

func TestScanSource_PendingRulesAreNotReportedAsFindings(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/cli/tinker.go": "package cli\n\n// tinker REPL\n",
	})
	rules := []Rule{{Feature: "Tinker REPL", Story: "S0.5", Patterns: []string{`(?i)\btinker\b`}}}

	found, err := ScanSource(root, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("pending rule produced findings: %v", found)
	}
}

func TestScanSource_AllowlistedPathsAreExempt(t *testing.T) {
	root := fixture(t, map[string]string{
		"README.md":            "Servlo is a fork of Lerd; the MCP server is gone.\n",
		"internal/cli/tool.go": "package cli // MCP\n",
	})
	rules := []Rule{{
		Feature: "MCP server", Story: "S0.4", Enforced: true,
		Patterns: []string{`(?i)\bmcp\b`},
		Allow:    []string{"README.md"},
	}}

	found, err := ScanSource(root, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Path != "internal/cli/tool.go" {
		t.Fatalf("got %v, want only internal/cli/tool.go", found)
	}
}

func TestScanSource_SkipsGeneratedAndVendoredTrees(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/ui/web/node_modules/pkg/index.js": "// mcp\n",
		"internal/ui/web/dist/app.js":               "// mcp\n",
		"docs/.vitepress/dist/index.html":           "<!-- mcp -->\n",
		"build/servlo":                              "mcp\n",
	})
	rules := []Rule{{Feature: "MCP server", Story: "S0.4", Enforced: true, Patterns: []string{`(?i)\bmcp\b`}}}

	found, err := ScanSource(root, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("scanned a generated tree: %v", found)
	}
}

func TestScanSource_MatchesFileNamesNotJustContents(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/php/versions_darwin.go": "package php\n",
	})
	rules := []Rule{{Feature: "macOS code paths", Story: "S0.2", Enforced: true, Patterns: []string{`_darwin\.go$`}}}

	found, err := ScanSource(root, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Line != 0 {
		t.Fatalf("got %v, want one path-level finding", found)
	}
}

func TestSplitCamel(t *testing.T) {
	cases := map[string]string{
		"startTray":            "start Tray",
		"MCPInject":            "MCP Inject",
		"refreshGlobalMCPSkil": "refresh Global MCP Skil",
		"plain text":           "plain text",
	}
	for in, want := range cases {
		if got := splitCamel(in); got != want {
			t.Errorf("splitCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRules_EveryRuleCompilesAndNamesItsStory(t *testing.T) {
	for _, r := range Rules() {
		if r.Feature == "" || r.Story == "" {
			t.Errorf("rule %+v must name both a feature and the story that owns it", r)
		}
		if len(r.Patterns) == 0 {
			t.Errorf("rule %q has no patterns", r.Feature)
		}
		if _, err := compile(r); err != nil {
			t.Errorf("rule %q: %v", r.Feature, err)
		}
	}
}

// TestSurface is the gate itself: every enforced rule must find nothing in the
// tree. Pending rules are logged so the remaining Phase 0 deletions stay visible.
func TestSurface(t *testing.T) {
	root := repoRoot(t)

	found, err := ScanSource(root, Rules())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s reappeared (%s owns its deletion): %s:%d: %s", f.Rule, f.Story, f.Path, f.Line, f.Text)
	}

	var pending []string
	for _, r := range Rules() {
		if !r.Enforced {
			pending = append(pending, r.Story+" "+r.Feature)
		}
	}
	if len(pending) > 0 {
		t.Logf("not yet enforced, pending their story: %s", strings.Join(pending, "; "))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// The routes are part of the deleted surface, not only the Go plumbing that
// served them. The original dump-bridge patterns described how the bridge was
// built, so the docs demo went on stubbing its endpoints long after nothing
// answered them, and nothing noticed until an unrelated fixture was deleted and
// the demo stopped building.
func TestRules_DumpBridgeCoversItsHTTPRoutes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("stub.ts", "const routes = { '/api/dumps/status': x, '/api/devtools/status': y };\n")

	found, err := ScanSource(dir, Rules())
	if err != nil {
		t.Fatal(err)
	}

	var hits int
	for _, f := range found {
		if strings.Contains(f.Rule, "dump") {
			hits++
		}
	}
	if hits == 0 {
		t.Errorf("the dump bridge's HTTP routes are not part of its rule: %+v", found)
	}
}

// The gate must not depend on whether somebody has built the docs demo. That
// output is gitignored, so a clean checkout passed and a built one failed on
// strings inside minified vendor chunks, which is the kind of flake that
// teaches people to stop trusting the scan.
func TestScanSource_SkipsTheGeneratedDemoBundle(t *testing.T) {
	dir := t.TempDir()
	generated := filepath.Join(dir, "docs", "public", "demo", "assets")
	if err := os.MkdirAll(generated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generated, "index-abc123.js"), []byte("var mcp='tray';auto_prepend;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := ScanSource(dir, Rules())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("the generated demo bundle was scanned: %+v", found)
	}

	// The demo's source is a different matter, and still scanned.
	src := filepath.Join(dir, "internal", "ui", "web", "demo")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "stubs.ts"), []byte("'/api/dumps/status'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err = ScanSource(dir, Rules())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Error("the demo's own source is not scanned, so a deleted feature could live there")
	}
}

// Every kind of file in the repository is either scanned or deliberately not.
//
// textExts was written once and never revisited, and the two file kinds added
// since sat outside it: the Containerfiles that decide what is inside the PHP
// images and the templates that generate the FPM unit and every nginx vhost.
// Those are the artefacts CLAUDE.md §3.1 points at by name, and the FrankenPHP
// image definition spent that whole time describing an Xdebug ini it arms and an
// mkcert CA it injects, with the gate reading straight past it because of the
// file's extension.
//
// A blind spot nobody chose is the failure here. Choosing one is fine: add the
// extension to textExts, or to the list below with the reason.
func TestScannedExtensions_CoverEverySourceKindInTheRepository(t *testing.T) {
	// Deliberately unscanned, with why.
	unscanned := map[string]string{
		".png": "raster image", ".gif": "raster image",
		".svg":         "vector image; the path data matches short words by accident",
		".cast":        "asciinema recording, replayed rather than read",
		".webmanifest": "generated icon manifest",
		".sum":         "checksum database", ".mod": "module graph",
		".gitignore": "path patterns",
		".txt": "recorded `php -m` output from the published PHP images and two docs " +
			"fixtures, none of it servlo source. What that output says about the images " +
			"is a question for the images rather than for this scan.",
	}

	root := repoRoot(t)
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || skipPaths[filepath.ToSlash(rel)]) {
				return fs.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != "" {
			seen[ext] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for ext := range seen {
		if textExts[ext] || unscanned[ext] != "" {
			continue
		}
		t.Errorf("%s files are in this repository and the scan neither reads them nor says why not: add the extension to textExts, or to the unscanned list with the reason", ext)
	}
}
