package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// The TUI advertises its keybindings in three places and handles them in a
// fourth. Nothing tied the two together, so a key outlived its handler twice:
// D ("open the Debug window") survived S0.5 deleting the debug surface, and O
// ("open") survived S0.6 deleting the browser launchers. Both went on being
// printed in the footer, and both did nothing at all when pressed — the worst
// shape a keybinding can take, since the operator cannot tell a dead key from
// one that ran and reported nothing.
//
// These tests read the advertisements and the handlers out of the package
// itself, so removing a feature's handler without removing its chip fails here
// rather than in front of whoever presses it.

// handledKeys returns every key string the package's key switches accept.
//
// The marker is `switch msg.String()`: bubbletea's KeyMsg renders a keystroke
// through that method, and no other switch in the package is keyed on it. That
// is more precise than collecting every case clause, which would sweep in the
// worker-name and severity switches next door.
func handledKeys(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}
	keys := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				sw, ok := n.(*ast.SwitchStmt)
				if !ok || !isKeyStringSwitch(sw.Tag) {
					return true
				}
				for _, stmt := range sw.Body.List {
					clause, ok := stmt.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, expr := range clause.List {
						lit, ok := expr.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						if v, err := strconv.Unquote(lit.Value); err == nil {
							keys[v] = true
						}
					}
				}
				return true
			})
		}
	}
	if len(keys) == 0 {
		t.Fatal("found no `switch msg.String()` cases; the marker this test reads has moved")
	}
	return keys
}

// isKeyStringSwitch reports whether a switch tag is a call to .String() on
// something named msg.
func isKeyStringSwitch(tag ast.Expr) bool {
	call, ok := tag.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "String" {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	return ok && recv.Name == "msg"
}

// singleKeystroke reports whether an advertised label names one plain
// keystroke, which is the only form that maps onto a case string without
// interpretation.
//
// Deliberately narrow. The reference also carries glyph shorthands ("↑ ↓  j k",
// "ctrl+←→") and prose sub-rows, which name real bindings under names the key
// switch never sees; asserting on those would mean maintaining a glyph
// translation table whose drift is a worse problem than the one being solved.
// Every dead key found so far was a lone letter, and a lone letter is checked
// exactly.
func singleKeystroke(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) != 1 {
		return "", false
	}
	r, _ := utf8.DecodeRuneInString(s)
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '?' || r == '/' {
		return s, true
	}
	return "", false
}

func TestHelpReferenceKeysAreHandled(t *testing.T) {
	handled := handledKeys(t)
	var dead []string
	for _, section := range helpReference {
		for _, row := range section.rows {
			key, ok := singleKeystroke(row[0])
			if !ok || handled[key] {
				continue
			}
			dead = append(dead, key+" — "+section.title+": "+row[1])
		}
	}
	sort.Strings(dead)
	for _, d := range dead {
		t.Errorf("help reference advertises a key with no handler: %s", d)
	}
}

func TestFooterChipKeysAreHandled(t *testing.T) {
	handled := handledKeys(t)
	m := &Model{width: 200}
	var dead []string
	for _, tab := range []topTab{tabDashboard, tabSites, tabServices} {
		m.activeTab = tab
		for _, chip := range m.footerChips() {
			key, ok := singleKeystroke(chip.key)
			if !ok || handled[key] {
				continue
			}
			dead = append(dead, key+" ("+chip.label+")")
		}
	}
	sort.Strings(dead)
	for _, d := range dead {
		t.Errorf("footer advertises a key with no handler: %s", d)
	}
}
