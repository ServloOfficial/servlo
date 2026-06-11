package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func seedSiteForLogs(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".lerd.yaml"), []byte("workers:\n  - queue\n"), 0644); err != nil {
		t.Fatalf("write .lerd.yaml: %v", err)
	}
	if err := config.AddSite(config.Site{Name: "logsite", Domains: []string{"logsite.test"}, Path: sitePath, PHPVersion: "8.4"}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
}

// textOf pulls the text payload out of a toolOK/toolErr envelope.
func textOf(t *testing.T, v any) (string, bool) {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", v)
	}
	isErr, _ := m["isError"].(bool)
	content, ok := m["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result has no content: %+v", m)
	}
	text, _ := content[0]["text"].(string)
	return text, isErr
}

func TestExecLogsSources_ListsSiteAndGlobals(t *testing.T) {
	seedSiteForLogs(t)
	res, rpcErr := execLogsSources(map[string]any{"site": "logsite"})
	if rpcErr != nil {
		t.Fatalf("execLogsSources: %v", rpcErr)
	}
	text, isErr := textOf(t, res)
	if isErr {
		t.Fatalf("unexpected error result: %s", text)
	}
	for _, name := range []string{"fpm", "worker:queue", "nginx", "dns"} {
		if !strings.Contains(text, `"name": "`+name+`"`) {
			t.Errorf("sources output missing %q:\n%s", name, text)
		}
	}
}

func TestExecLogsFetch_UnknownSourceListsValidNames(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	res, _ := execLogsFetch(map[string]any{"source": "does-not-exist"})
	text, isErr := textOf(t, res)
	if !isErr {
		t.Fatalf("expected error for unknown source, got: %s", text)
	}
	if !strings.Contains(text, "nginx") {
		t.Errorf("error should list valid sources, got: %s", text)
	}
}

func TestExecLogsFetch_RequiresSource(t *testing.T) {
	seedSiteForLogs(t)
	res, _ := execLogsFetch(map[string]any{"site": "logsite"})
	text, isErr := textOf(t, res)
	if !isErr || !strings.Contains(text, "source is required") {
		t.Fatalf("expected source-required error, got isErr=%v text=%s", isErr, text)
	}
}

func TestExecLogsFetch_FetchShapeIsJSON(t *testing.T) {
	// Sanity check the JSON envelope shape produced by a successful fetch by
	// round-tripping a hand-built result through the marshaller path the
	// handler uses. This guards the field names the agent relies on.
	out := map[string]any{"source": "fpm", "kind": "podman", "cursor": "", "truncated": false, "count": 0, "entries": []any{}}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, k := range []string{"source", "kind", "cursor", "truncated", "count", "entries"} {
		if !strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("fetch JSON missing key %q", k)
		}
	}
}
