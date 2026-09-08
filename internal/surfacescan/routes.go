package surfacescan

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// The permission half of the scan.
//
// The deleted-feature half reads the tree and fails when a name comes back.
// This half reads the routes the panel registers and fails when one of them
// does not declare what authority it needs.
//
// It exists because the enforcement in internal/authz is only as good as
// somebody remembering to classify a new route, and "remember to think about
// roles" is not a mechanism. Deny by default already makes an undeclared route
// fail closed for a Developer, which is the safe direction; this makes it fail
// at build time instead, so the person adding the route finds out rather than
// the person using it.

// The panel dispatches a path in two shapes, and both count as a route.
//
// Most are registered on the mux. The authentication routes are dispatched by
// the middleware in front of it, because they have to answer before there is a
// session for the mux to be reached with. Reading only the first shape would
// leave the second undeclared and unnoticed.
var routePatterns = []*regexp.Regexp{
	regexp.MustCompile(`mux\.Handle(?:Func)?\("([^"]+)"`),
	regexp.MustCompile(`^\s*case "(/[^"]+)":`),
}

// routesIn returns every path a file dispatches, with the line it is on.
func routesIn(source string) []UndeclaredRoute {
	var found []UndeclaredRoute
	for n, line := range strings.Split(source, "\n") {
		for _, re := range routePatterns {
			for _, match := range re.FindAllStringSubmatch(line, -1) {
				found = append(found, UndeclaredRoute{Pattern: match[1], Line: n + 1})
			}
		}
	}
	return found
}

// UndeclaredRoute is one route with no permission behind it.
type UndeclaredRoute struct {
	Pattern string
	File    string
	Line    int
}

// ScanRoutes reads the route registrations out of the named files and returns
// those with no entry in declared.
//
// declared is passed in rather than imported, so this package stays free of
// internal/authz and the scan does not become a reason those two packages have
// to know about each other.
func ScanRoutes(files []string, declared map[string]bool) ([]UndeclaredRoute, error) {
	var missing []UndeclaredRoute
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		for _, route := range routesIn(string(data)) {
			// The Svelte app's catch-all serves the page shell for every path
			// the client routes itself, and carries no data of its own.
			if route.Pattern == "/" || declared[route.Pattern] {
				continue
			}
			route.File = path
			missing = append(missing, route)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Pattern < missing[j].Pattern })
	return missing, nil
}

// StaleDeclarations returns declared patterns that no route registers.
//
// A registry entry for a route that no longer exists is not harmless: it is a
// permission somebody will read as describing the panel, and it will be wrong.
// It is also how a route quietly loses its declaration when it is renamed, with
// the old entry left behind to make the count look right.
func StaleDeclarations(files []string, declared map[string]bool) ([]string, error) {
	registered, err := RegisteredRoutes(files)
	if err != nil {
		return nil, err
	}
	var stale []string
	for pattern := range declared {
		if !registered[pattern] {
			stale = append(stale, pattern)
		}
	}
	sort.Strings(stale)
	return stale, nil
}

// exemptListStart opens the panel's cross-origin exemption list, and
// exemptEntry matches one path inside it.
var (
	exemptListStart = regexp.MustCompile(`^var csrfExemptPaths = \[\]string\{`)
	exemptEntry     = regexp.MustCompile(`^\s*"(/[^"]+)",`)
)

// CrossOriginExemptions returns the paths source exempts from the panel's
// cross-origin gate.
//
// It reads the declaration rather than importing internal/ui, which keeps this
// package free of the panel exactly as ScanRoutes keeps it free of authz.
// A file with no such declaration yields nothing, which the caller checks for:
// a scan that silently finds no exemptions proves nothing about the ones that
// are there.
func CrossOriginExemptions(source string) []string {
	var found []string
	inList := false
	for _, line := range strings.Split(source, "\n") {
		if !inList {
			inList = exemptListStart.MatchString(line)
			continue
		}
		if strings.HasPrefix(line, "}") {
			break
		}
		if m := exemptEntry.FindStringSubmatch(line); m != nil {
			found = append(found, m[1])
		}
	}
	return found
}

// RegisteredRoutes returns every path the named files dispatch.
func RegisteredRoutes(files []string) (map[string]bool, error) {
	registered := map[string]bool{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		for _, route := range routesIn(string(data)) {
			registered[route.Pattern] = true
		}
	}
	return registered, nil
}
