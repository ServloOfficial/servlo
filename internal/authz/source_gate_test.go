package authz

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Believing X-Real-IP rests on two things nothing else checks.
//
// The header decides which bucket a failed sign-in counts against and which
// address the audit log records for it. A request that can set it freely resets
// its own rate-limit budget on every attempt and writes whatever it likes into
// the log of who signed in from where.
//
// What makes it safe is narrow. Only a request arriving on the unix socket is
// marked, because only servlo's own nginx can reach that socket, and every
// config servlo writes that proxies to it overwrites the header with the peer
// address. Break either half and the header is forgeable, with nothing failing
// and nothing logged; a vhost that simply omits the proxy_set_header line hands
// the client's own value straight through.

// Only the unix-socket listener may mark a request as coming from servlo's own
// nginx. A second caller is a second thing that decides a header is true.
func TestWithOwnProxy_IsCalledOnlyByTheUnixSocketListener(t *testing.T) {
	callers := map[string]bool{
		// Where the listener sets it, alongside the loopback marker.
		"internal/ui/server.go": true,
	}
	root := moduleRoot(t)
	for _, path := range goFilesUnder(t, root) {
		rel := strings.TrimPrefix(path, root+string(os.PathSeparator))
		if strings.HasPrefix(rel, "internal/authz/") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "WithOwnProxy(") {
			continue
		}
		if !callers[filepath.ToSlash(rel)] {
			t.Errorf("%s calls WithOwnProxy, which tells servlo to believe X-Real-IP on that request. Only the unix-socket listener may", rel)
		}
	}
}

// Every generated nginx config that proxies to the panel socket has to set the
// header rather than pass along whatever arrived.
func TestPanelProxies_OverwriteTheRealIPHeader(t *testing.T) {
	root := moduleRoot(t)
	// A proxy_pass to a unix socket, and the setting of X-Real-IP, in the same
	// generated block. Both live in Go string literals, so this reads the
	// sources that write them rather than any file on disk.
	proxies := regexp.MustCompile(`proxy_pass http://unix:`)
	sets := regexp.MustCompile(`proxy_set_header X-Real-IP \$remote_addr`)

	checked := 0
	for _, path := range goFilesUnder(t, root) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !proxies.Match(body) {
			continue
		}
		checked++
		if !sets.Match(body) {
			rel := strings.TrimPrefix(path, root+string(os.PathSeparator))
			t.Errorf("%s writes an nginx config that proxies to a unix socket without setting X-Real-IP, so a client's own header reaches servlo as its source address", rel)
		}
	}
	if checked == 0 {
		t.Fatal("found no generated config proxying to the panel socket, so this checked nothing")
	}
}

func goFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}
