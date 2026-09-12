package ui

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/authz"
)

// The websocket pushes a snapshot of every site to every connection. Gating
// the routes and leaving that alone would mean a developer never able to open
// another team's site but able to watch it, which is the door beside the door
// S5.2 already closed once.
func TestSnapshotScope_DeveloperSeesOnlyTheirSites(t *testing.T) {
	sites := []byte(`[{"domain":"example.com","php":"8.4"},{"domain":"other.com","php":"8.3"}]`)
	scope := authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}}

	filtered := scopeSites(sites, scope)
	var got []map[string]any
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatalf("the filtered snapshot is not valid JSON: %v (%s)", err, filtered)
	}
	if len(got) != 1 || got[0]["domain"] != "example.com" {
		t.Fatalf("filtered sites = %s, want only example.com", filtered)
	}
}

func TestSnapshotScope_AdminSeesEverySite(t *testing.T) {
	sites := []byte(`[{"domain":"example.com"},{"domain":"other.com"}]`)
	filtered := scopeSites(sites, authz.Scope{Role: authz.RoleAdmin})
	if string(filtered) != string(sites) {
		t.Errorf("an admin's snapshot was filtered: %s", filtered)
	}
}

// A developer with nothing assigned sees an empty list, not every site and not
// a broken frame.
func TestSnapshotScope_DeveloperWithNoSitesSeesAnEmptyList(t *testing.T) {
	sites := []byte(`[{"domain":"example.com"},{"domain":"other.com"}]`)
	filtered := scopeSites(sites, authz.Scope{Role: authz.RoleDeveloper})

	var got []map[string]any
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatalf("not valid JSON: %v (%s)", err, filtered)
	}
	if len(got) != 0 {
		t.Errorf("filtered sites = %s, want an empty list", filtered)
	}
}

// A snapshot servlo cannot parse is not one to pass through unfiltered. That
// would make a malformed payload the way to see everything.
func TestSnapshotScope_UnparseableSnapshotBecomesEmpty(t *testing.T) {
	for _, payload := range []string{`{"not":"an array"}`, `[`, `garbage`} {
		filtered := scopeSites([]byte(payload), authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}})
		if string(filtered) != "[]" {
			t.Errorf("an unparseable snapshot became %s, want an empty list", filtered)
		}
	}
}

// Services belong to the admin. A developer's frame carries none rather than
// carrying them and trusting the browser not to render them.
func TestSnapshotScope_DeveloperSeesNoServices(t *testing.T) {
	services := []byte(`[{"name":"mysql"},{"name":"redis"}]`)
	if got := scopeServices(services, authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}}); string(got) != "[]" {
		t.Errorf("a developer's frame carries services: %s", got)
	}
	if got := scopeServices(services, authz.Scope{Role: authz.RoleAdmin}); string(got) != string(services) {
		t.Errorf("an admin's services were filtered: %s", got)
	}
}

// Empty stays empty. A frame that carries no sites at all must not gain an
// empty array, because the client reads a missing key as "unchanged" and an
// empty one as "there are none".
func TestSnapshotScope_EmptyPayloadStaysEmpty(t *testing.T) {
	if got := scopeSites(nil, authz.Scope{Role: authz.RoleDeveloper}); len(got) != 0 {
		t.Errorf("an absent sites payload became %s", got)
	}
	if got := scopeServices(nil, authz.Scope{Role: authz.RoleDeveloper}); len(got) != 0 {
		t.Errorf("an absent services payload became %s", got)
	}
}

// The same door, two payloads further along the frame. /api/workers/health is
// PermAdmin, so a developer asking for it over HTTP is refused; the websocket
// pushed the identical list to that developer in the opening snapshot and in
// every frame after it. What it carries is not just the fact that another
// team's worker is unhealthy: it is that site's name, its worker and unit
// names, and the last line out of its journal.
func TestSnapshotScope_DeveloperIsNotToldAboutOtherSitesWorkers(t *testing.T) {
	workers := []byte(`[{"site":"other","worker":"queue","unit":"servlo-other-queue.service","state":"failed","last_error":"SQLSTATE[HY000] password authentication failed"}]`)
	scope := authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}}

	got := scopeUnhealthyWorkers(workers, scope)
	if string(got) != "[]" {
		t.Errorf("a developer was sent another site's worker failures: %s", got)
	}
}

func TestSnapshotScope_AdminStillSeesEveryUnhealthyWorker(t *testing.T) {
	workers := []byte(`[{"site":"other","worker":"queue","state":"failed"}]`)
	got := scopeUnhealthyWorkers(workers, authz.Scope{Role: authz.RoleAdmin})
	if string(got) != string(workers) {
		t.Errorf("an admin's worker health was filtered: %s", got)
	}
}

// A notification names the site it is about, in its title, its tag and its
// data. Broadcasting one to every peer tells a developer that a site they
// cannot open exists, which route on it is slow, and how slow.
func TestSnapshotScope_DeveloperIsNotNotifiedAboutOtherSites(t *testing.T) {
	scope := authz.Scope{Role: authz.RoleDeveloper, Sites: []string{"example.com"}}

	other := []byte(`{"kind":"slow_route","title":"Slow route on other","data":{"site":"other.com","route":"/checkout"}}`)
	if got := scopeNotification(other, scope); got != nil {
		t.Errorf("a developer was notified about another site: %s", got)
	}

	own := []byte(`{"kind":"slow_route","title":"Slow route on example","data":{"site":"example.com","route":"/cart"}}`)
	if got := scopeNotification(own, scope); string(got) != string(own) {
		t.Errorf("a developer lost a notification about their own site: %s", got)
	}

	// A notification that names no site is about the server, not a tenant.
	server := []byte(`{"kind":"test","title":"Test notification"}`)
	if got := scopeNotification(server, scope); string(got) != string(server) {
		t.Errorf("a site-less notification was dropped: %s", got)
	}
}

// The helpers above were never the defect. Two of them did not exist and the
// frame handed the payload straight out, which no test of a helper can catch.
// So this reads the frame itself: every payload a wsMessage carries is either
// passed through a scope function or named here as deliberately whole, and a
// sixth field added later fails until somebody decides which it is.
func TestSnapshotFrame_EveryPayloadIsScopedOrDeclaredWhole(t *testing.T) {
	// Whole on purpose, with the reason it is safe.
	whole := map[string]string{
		// /api/status is PermSelf, so the route already answers a developer.
		"Status": "the route beside it is PermSelf",
		// Not a payload: the list of which payloads this frame carries.
		"Kinds": "not content",
	}

	broker, err := os.ReadFile("ws_broker.go")
	if err != nil {
		t.Fatal(err)
	}
	fields := payloadFields(t, string(broker))
	if len(fields) < 4 {
		t.Fatalf("found %d payload fields on wsMessage, so this proves nothing", len(fields))
	}

	ws, err := os.ReadFile("ws.go")
	if err != nil {
		t.Fatal(err)
	}
	frames := string(ws)
	for _, field := range fields {
		if _, ok := whole[field]; ok {
			continue
		}
		// Every carried payload has a scope function named for it.
		if !strings.Contains(frames, "scope"+field+"(") {
			t.Errorf("wsMessage.%s reaches the browser unscoped: add scope%s and call it in the frame, or declare it whole with a reason", field, field)
		}
	}
}

// payloadFields returns the field names of the wsMessage struct.
func payloadFields(t *testing.T, src string) []string {
	t.Helper()
	start := strings.Index(src, "type wsMessage struct {")
	if start < 0 {
		t.Fatal("wsMessage is not declared in ws_broker.go any more")
	}
	body := src[start:]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatal("wsMessage has no closing brace")
	}
	var out []string
	for _, line := range strings.Split(body[:end], "\n")[1:] {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "//") {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

// Every payload the socket broadcasts has to have been narrowed, or to have a
// written reason it needs no narrowing. The check is against the send path
// rather than only against the table, because a field can be declared here and
// still be handed to assembleSnapshot whole.
func TestEveryWebsocketPayloadIsScoped(t *testing.T) {
	msgType := reflect.TypeOf(wsMessage{})
	for i := 0; i < msgType.NumField(); i++ {
		name := msgType.Field(i).Name
		if _, ok := wsPayloadScoping[name]; !ok {
			t.Errorf("wsMessage.%s goes to every open dashboard and no scoping decision was recorded for it: "+
				"add it to wsPayloadScoping, with the function that narrows it or the reason it needs none", name)
		}
	}
	for name := range wsPayloadScoping {
		if _, ok := msgType.FieldByName(name); !ok {
			t.Errorf("wsPayloadScoping decides about %s, which wsMessage no longer carries", name)
		}
	}

	whole := map[string]bool{}
	for name, decision := range wsPayloadScoping {
		if strings.HasPrefix(decision, "whole:") {
			whole[name] = true
		}
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "ws.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing ws.go: %v", err)
	}

	calls := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "assembleSnapshot" {
			return true
		}
		calls++
		// The last argument is the frame kinds; everything before it is a
		// payload that reaches a browser.
		for _, arg := range call.Args[:len(call.Args)-1] {
			if scopedArgument(arg, whole) {
				continue
			}
			t.Errorf("%s: a snapshot payload reaches every connection unscoped: %s",
				fset.Position(arg.Pos()), exprText(arg))
		}
		return true
	})
	if calls == 0 {
		t.Fatal("no assembleSnapshot call found, so this check proved nothing")
	}
}

// scopedArgument reports whether one argument to assembleSnapshot is safe to
// send: nothing at all, the result of a scope function, or a payload the table
// says goes whole.
func scopedArgument(arg ast.Expr, whole map[string]bool) bool {
	switch e := arg.(type) {
	case *ast.Ident:
		return e.Name == "nil"
	case *ast.CallExpr:
		switch fn := e.Fun.(type) {
		case *ast.Ident:
			return strings.HasPrefix(fn.Name, "scope")
		case *ast.SelectorExpr:
			// snapshots.Status() and the like: a reader on the snapshot store.
			return whole[fn.Sel.Name]
		}
	case *ast.SelectorExpr:
		// msg.Status and the like.
		return whole[e.Sel.Name]
	}
	return false
}

func exprText(e ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, token.NewFileSet(), e)
	return b.String()
}
