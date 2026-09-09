package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// fixKeyConstants maps the names ApplyDoctorFix switches on to their values, so
// the source scan below can ask heavyFixKeys about a case it just read. A fix
// constant missing from here fails the test rather than being skipped: an
// unknown name is exactly the new fix nobody classified.
var fixKeyConstants = map[string]string{
	"fixMkdir":        fixMkdir,
	"fixEnableLinger": fixEnableLinger,
	"fixPhpRebuild":   fixPhpRebuild,
	"fixNetworkWait":  fixNetworkWait,
	"fixInstall":      fixInstall,
	"fixCleanup":      fixCleanup,
	"fixDNSRepair":    fixDNSRepair,
}

// A fix that re-enters a servlo subcommand is a whole operation with its own
// blast radius, and heavyFixKeys is what makes `--fix --yes` stop and ask about
// one. The list was hand-kept and had drifted: php:rebuild rebuilds an image and
// restarts every FPM and worker unit on that PHP version, and ran unprompted.
//
// Read out of ApplyDoctorFix rather than restated, so a fix added as a
// subcommand cannot be added without classifying it.
func TestHeavyFixKeys_CoverEveryFixThatReEntersASubcommand(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "doctor_fix.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing doctor_fix.go: %v", err)
	}

	var apply *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "ApplyDoctorFix" {
			apply = fn
		}
	}
	if apply == nil {
		t.Fatal("ApplyDoctorFix is gone; this test guards its switch and has to follow it")
	}

	checked := 0
	ast.Inspect(apply, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok || len(clause.List) == 0 {
			return true
		}
		if !callsReEntry(clause) {
			return true
		}
		for _, expr := range clause.List {
			name, ok := expr.(*ast.Ident)
			if !ok {
				t.Errorf("a case on something other than a fix constant at %s", fset.Position(expr.Pos()))
				continue
			}
			key, known := fixKeyConstants[name.Name]
			if !known {
				t.Fatalf("%s is a fix constant this test has never heard of: add it to fixKeyConstants and decide whether it is heavy", name.Name)
			}
			checked++
			if !heavyFixKeys[key] {
				t.Errorf("the %q fix re-enters a servlo subcommand and runs unprompted under --yes: add it to heavyFixKeys", key)
			}
		}
		return true
	})
	if checked == 0 {
		t.Fatal("no fix in ApplyDoctorFix re-enters a subcommand any more, so this test is watching nothing")
	}
}

// callsReEntry reports whether a case body runs another servlo subcommand.
func callsReEntry(clause *ast.CaseClause) bool {
	found := false
	for _, stmt := range clause.Body {
		ast.Inspect(stmt, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "runSelf" {
				found = true
			}
			return true
		})
	}
	return found
}
