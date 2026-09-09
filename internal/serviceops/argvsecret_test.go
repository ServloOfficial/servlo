package serviceops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A credential must never be spelled into a podman argument.
//
// /proc/<pid>/cmdline is readable by every process on the machine, and on this
// one every site runs as the same Linux user (PRD section 6), so a password in
// an argument is a password every site can read for as long as the command
// runs. Listing a service's databases happens on a panel page load, so the
// window is not rare.
//
// podman reads `--env NAME` out of its own environment, so the value travels in
// the calling process rather than in an argument list. internal/dbexec has
// always done it that way. Everywhere else spelled the value, and three of
// those places carried a comment saying they did not: containerExec said
// secrets do not leak into /proc/<pid>/cmdline, dumpToHost said they stay out
// of argv, and a CLI test called itself PasswordOnlyInEnv while asserting the
// password was in the argv.
//
// So the rule is checked rather than described: a podman `--env` or `-e`
// argument never spells a credential's value.
//
// It is the credentials this is about, not every variable. A path like HOME or
// SSH_AUTH_SOCK in an argument tells a reader nothing they could not get from
// the process table anyway, and forwarding those by name would mean putting
// each computed path on the process for no gain.
func TestPodmanEnvArgsNeverSpellACredential(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for i, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if lit.Value != `"--env"` && lit.Value != `"-e"` {
					continue
				}
				if i+1 >= len(call.Args) {
					continue
				}
				next := call.Args[i+1]
				// A literal that already carries an "=" is a spelled pair.
				if nl, ok := next.(*ast.BasicLit); ok && nl.Kind == token.STRING {
					if isCredentialPair(nl.Value) {
						t.Errorf("%s:%d passes a spelled credential to podman --env: %s\nforward the name and put the value on the command's Env, so it is not in /proc",
							rel, fset.Position(nl.Pos()).Line, nl.Value)
					}
					continue
				}
				// A concatenation naming a credential, e.g. "PGPASSWORD="+pw.
				if bin, ok := next.(*ast.BinaryExpr); ok {
					if nl, ok := bin.X.(*ast.BasicLit); ok && isCredentialPair(nl.Value) {
						t.Errorf("%s:%d builds a spelled credential for podman --env: %s...\nforward the name and put the value on the command's Env, so it is not in /proc",
							rel, fset.Position(bin.Pos()).Line, nl.Value)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
}

// isCredentialPair reports whether a "NAME=" literal names something secret.
// The list is the shapes servlo actually hands a database or object-store
// client, plus the words a new one would be spelled with.
func isCredentialPair(lit string) bool {
	name, _, ok := strings.Cut(strings.Trim(lit, `"`), "=")
	if !ok {
		return false
	}
	upper := strings.ToUpper(name)
	for _, mark := range []string{"PWD", "PASSWORD", "PASS", "SECRET", "TOKEN", "KEY", "CREDENTIAL"} {
		if strings.Contains(upper, mark) {
			return true
		}
	}
	return false
}
