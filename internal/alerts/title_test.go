package alerts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// Every kind needs a heading, because title() falls back to the raw slug and an
// operator reading "backup_none" in the panel and in an inbox subject is being
// shown servlo's vocabulary instead of a sentence. The kinds are read out of
// the source rather than listed here, so adding one and forgetting its heading
// fails this test rather than shipping.
func TestTitle_EveryKindHasAHeading(t *testing.T) {
	kinds := declaredKinds(t)
	if len(kinds) < 5 {
		t.Fatalf("found %d kinds in the source, which means this test is reading the wrong thing", len(kinds))
	}
	for name, value := range kinds {
		if got := (Alert{Kind: value}).Title(); got == value {
			t.Errorf("%s (%q) has no heading, so the panel shows the slug", name, value)
		}
	}
}

// declaredKinds is every Kind* constant in this package, by name and value.
func declaredKinds(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "alerts.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "Kind") || i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			out[name.Name] = value
		}
		return true
	})
	return out
}
