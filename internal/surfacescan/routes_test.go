package surfacescan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/authz"
)

// panelRouteFiles are the files that register the panel's routes. One place,
// so a second mux somewhere would show up as routes this scan never sees, and
// this list is what a reviewer checks when that is suspected.
func panelRouteFiles(t *testing.T) []string {
	t.Helper()
	return []string{
		filepath.Join(repoRoot(t), "internal", "ui", "server.go"),
		// The authentication routes are dispatched by the middleware in front
		// of the mux, because they have to answer before there is a session.
		filepath.Join(repoRoot(t), "internal", "ui", "panel_auth.go"),
	}
}

func declaredPatterns() map[string]bool {
	declared := map[string]bool{}
	for pattern := range authz.Permissions() {
		declared[pattern] = true
	}
	return declared
}

// The gate: every route the panel registers declares a permission.
//
// Deny by default already makes an undeclared route fail closed for a
// Developer, which is the safe direction. This makes it fail at build time
// instead, so the person adding the route finds out rather than the person
// using it.
func TestEveryRouteDeclaresAPermission(t *testing.T) {
	missing, err := ScanRoutes(panelRouteFiles(t), declaredPatterns())
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range missing {
		t.Errorf("%s:%d registers %q with no permission declared. Add it to authz.Permissions().",
			route.File, route.Line, route.Pattern)
	}
}

// A declaration for a route that no longer exists is not harmless: it is a
// permission somebody will read as describing the panel, and it will be wrong.
// It is also how a renamed route quietly loses its declaration, with the old
// entry left behind to make the count look right.
func TestNoStalePermissionDeclarations(t *testing.T) {
	stale, err := StaleDeclarations(panelRouteFiles(t), declaredPatterns())
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range stale {
		t.Errorf("authz.Permissions() declares %q, which no route registers", pattern)
	}
}

// The scanner has to actually find routes, or both checks above pass by
// looking at nothing.
func TestScanRoutes_FindsTheRoutesThatAreThere(t *testing.T) {
	found, err := ScanRoutes(panelRouteFiles(t), map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) < 20 {
		t.Fatalf("the route scanner found %d routes, which is too few to be reading the real file", len(found))
	}
}

// And it has to notice one that is missing, or it is a check that cannot fail.
func TestScanRoutes_NoticesAnUndeclaredRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.go")
	source := `package ui
func routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/declared", handleOne)
	mux.HandleFunc("/api/forgotten", handleTwo)
	mux.Handle("/api/also-forgotten", handleThree)
}`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	missing, err := ScanRoutes([]string{path}, map[string]bool{"/api/declared": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 {
		t.Fatalf("found %d undeclared routes, want 2: %+v", len(missing), missing)
	}
	if missing[0].Pattern != "/api/also-forgotten" || missing[1].Pattern != "/api/forgotten" {
		t.Errorf("undeclared routes = %+v", missing)
	}
	if missing[1].Line != 4 {
		t.Errorf("line = %d, want 4", missing[1].Line)
	}
}

// The Svelte app's catch-all serves the page shell for every path the client
// routes itself and carries no data, so it is not a route to declare.
func TestScanRoutes_IgnoresTheAppShell(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.go")
	if err := os.WriteFile(path, []byte(`mux.Handle("/", serveSvelte())`), 0o644); err != nil {
		t.Fatal(err)
	}
	missing, err := ScanRoutes([]string{path}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Errorf("the app shell was reported as undeclared: %+v", missing)
	}
}

// The third gate: every path the panel exempts from the cross-origin check is
// a route the panel registers.
//
// The exemption list is the one place a route is named without being served,
// so it is the one place a name can outlive the endpoint. It did: the
// laptop-bootstrap endpoint was exempt here, described in the gate's comments
// and in `servlo remote-control`'s help as having a token, a source-IP and a
// lockout gate of its own, and never registered on the mux at all. Nothing
// failed, because nothing checks that a route named is a route served.
func TestEveryCrossOriginExemptionIsARegisteredRoute(t *testing.T) {
	gate := filepath.Join(repoRoot(t), "internal", "ui", "crossorigin.go")
	source, err := os.ReadFile(gate)
	if err != nil {
		t.Fatal(err)
	}
	exempt := CrossOriginExemptions(string(source))
	if len(exempt) == 0 {
		t.Fatalf("%s declares no cross-origin exemptions — the scanner found nothing "+
			"to check, which is not the same as finding nothing wrong. Has "+
			"csrfExemptPaths been renamed or reshaped?", gate)
	}

	registered, err := RegisteredRoutes(panelRouteFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range exempt {
		if !registered[path] {
			t.Errorf("%s exempts %q from the cross-origin gate, but no panel route "+
				"registers it. Either serve it or drop the exemption.", gate, path)
		}
	}
}

// The scanner reports a stale exemption rather than passing over it.
func TestCrossOriginExemptions_ReadsTheList(t *testing.T) {
	source := `var csrfExemptPaths = []string{
	"/api/internal/notify",
	"/api/remote-setup",
}
`
	got := CrossOriginExemptions(source)
	want := []string{"/api/internal/notify", "/api/remote-setup"}
	if len(got) != len(want) {
		t.Fatalf("exemptions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("exemption %d = %q, want %q", i, got[i], want[i])
		}
	}
}
