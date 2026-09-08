package siteops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// FinishLink writes a site's artifacts. It does not register the site, and the
// name does not say so: "finish the link" reads like the whole of linking.
//
// Registering happens a layer up, in linker.Apply, which every path that goes
// through `servlo link` reaches. Two paths did not — the one-click app
// installer and `servlo import` — and both wrote a pool, a vhost and a quadlet
// for a site that was never in sites.yaml. nginx served them; nothing else knew
// they existed, so no backups, no cron, and `servlo secure` could not find the
// domain to get a certificate for.
//
// This walks the tree rather than trusting the next person to remember: a
// package that calls FinishLink itself, instead of going through linker, has
// taken on the registration too.
func TestEveryCallerOfFinishLinkAlsoRegistersTheSite(t *testing.T) {
	root := repoRoot(t)

	// linker owns the registration for everything that goes through it, and
	// siteops is where FinishLink lives.
	exempt := map[string]bool{"linker": true, "siteops": true}

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dist", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// A cheap text pre-filter so the parser only runs on candidate files.
		if !strings.Contains(string(src), "FinishLink") && !strings.Contains(string(src), "FinishSiteOnly") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, src, parser.ParseComments)
		if parseErr != nil {
			return parseErr
		}
		if exempt[file.Name.Name] {
			return nil
		}
		// Both halves read the code rather than the file's text. Two earlier
		// cuts of this did not work. Matching the string "AddSite" was
		// worthless, because the comment explaining why the call is there
		// contains the word, so a file passed by describing the fix rather than
		// applying it. Matching a call to FinishLink then missed the installer
		// entirely, which assigns it to a variable and calls that instead. So
		// this counts references, which a comment is not.
		if !references(file, "FinishLink", "FinishSiteOnly") {
			return nil
		}
		if !references(file, "AddSite") {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("%s writes a site's artifacts through FinishLink but never calls AddSite, so the site it creates is not in the registry", o)
	}
}

// references reports whether the file names any of these identifiers in its
// code — called, assigned to a variable, or passed along. Comments and string
// literals are not identifiers, so they do not count, which is the point.
func references(file *ast.File, names ...string) bool {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if want[node.Sel.Name] {
				found = true
			}
		case *ast.Ident:
			if want[node.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root")
	return ""
}
