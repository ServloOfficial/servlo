package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeHTTPOverride seeds the user http-level override file.
func writeHTTPOverride(t *testing.T, tmp, body string) {
	t.Helper()
	dir := filepath.Join(tmp, "servlo", "nginx", "http.d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir http.d: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zz-servlo-user.conf"), []byte(body), 0644); err != nil {
		t.Fatalf("write override: %v", err)
	}
}

func TestHTTPOverrideNames(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	writeHTTPOverride(t, tmp, `# a comment
client_max_body_size 100m;
  gzip on;   # trailing comment
# sendfile off;

map $http_referer $foo {
    default          0;
    keepalive_timeout 9;
}
`)
	got := httpOverrideNames()
	for _, want := range []string{"client_max_body_size", "gzip", "map"} {
		if !got[want] {
			t.Errorf("expected %q in override names, got %v", want, got)
		}
	}
	for _, unwanted := range []string{"sendfile", "keepalive_timeout", "default", "#"} {
		if got[unwanted] {
			t.Errorf("did not expect %q in override names, got %v", unwanted, got)
		}
	}
}

func TestHTTPOverrideNames_missingDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if got := httpOverrideNames(); len(got) != 0 {
		t.Errorf("expected no names without an override file, got %v", got)
	}
}

// A user override of a directive servlo already sets in http{} must remove
// servlo's default: nginx rejects a duplicate simple directive in the same
// context instead of letting the later one win (issue #1066).
func TestEnsureNginxConfig_dropsOverriddenDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	writeHTTPOverride(t, tmp, "client_max_body_size 100m;\n")
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	body := readRenderedConf(t, tmp)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "client_max_body_size ") {
			t.Fatalf("servlo default still active, nginx would reject it as duplicate:\n%s", body)
		}
	}
	if !strings.Contains(body, "# client_max_body_size 0;") {
		t.Errorf("expected the dropped default to stay visible as a comment, got:\n%s", body)
	}
	if !strings.Contains(body, "include /etc/nginx/http.d/*.conf;") {
		t.Errorf("http.d include must survive the filter, got:\n%s", body)
	}
	if !strings.Contains(body, "sendfile on;") {
		t.Errorf("untouched defaults must survive the filter, got:\n%s", body)
	}
}

// log_format and access_log may repeat in the same context, so a user
// declaring either must not retire servlo's own: nginx then fails to start
// with "unknown log format servlo_access" and every site goes down.
func TestEnsureNginxConfig_keepsRepeatableLogDirectives(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	writeHTTPOverride(t, tmp, "log_format main '$remote_addr $status';\naccess_log /var/log/nginx/access.log main;\n")
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	body := readRenderedConf(t, tmp)
	if !hasActiveDirective(body, "log_format servlo_access ") {
		t.Errorf("servlo's log_format must survive a user log_format, got:\n%s", body)
	}
	if !hasActiveDirective(body, "access_log syslog:") {
		t.Errorf("servlo's access_log feeds the request stats, got:\n%s", body)
	}
}

// hasActiveDirective reports whether the conf carries prefix as a live
// directive rather than one the filter commented out.
func hasActiveDirective(conf, prefix string) bool {
	for _, line := range strings.Split(conf, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

// The filter only applies to servlo's own http{} defaults. A user directive that
// happens to share a name with something in another block (events{}, or the
// nested location blocks of a vhost) must not disturb it.
func TestEnsureNginxConfig_onlyFiltersHTTPLevel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	writeHTTPOverride(t, tmp, "worker_connections 4096;\n")
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	if body := readRenderedConf(t, tmp); !strings.Contains(body, "worker_connections 1024;") {
		t.Errorf("events{} directive must not be filtered, got:\n%s", body)
	}
}

// Removing the override (the Reset flow) must bring servlo's defaults back.
func TestEnsureNginxConfig_restoresDefaultsAfterReset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	writeHTTPOverride(t, tmp, "client_max_body_size 100m;\n")
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	if err := os.Remove(filepath.Join(tmp, "servlo", "nginx", "http.d", "zz-servlo-user.conf")); err != nil {
		t.Fatalf("remove override: %v", err)
	}
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	if body := readRenderedConf(t, tmp); !strings.Contains(body, "client_max_body_size 0;") {
		t.Errorf("expected the default back after reset, got:\n%s", body)
	}
}

func readRenderedConf(t *testing.T, tmp string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(tmp, "servlo", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read rendered nginx.conf: %v", err)
	}
	return string(body)
}

// A map is the standard nginx idiom for anything conditional, so an operator
// writing one into the http-level override is ordinary. It is also repeatable,
// and servlo has one of its own: the websocket upgrade map every proxying vhost
// reads $connection_upgrade from.
//
// Retiring a block by commenting out its first line leaves its body and its
// closing brace behind as http-level statements, which nginx refuses. The file
// is only read when nginx starts or reloads, so the sites keep serving on the
// configuration already in memory and the machine comes up with nothing on it
// whenever it is next restarted.
func TestEnsureNginxConfig_keepsItsOwnMapWhenTheOperatorWritesOne(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	writeHTTPOverride(t, tmp, "map $http_user_agent $is_bot {\n    default 0;\n    ~*bot   1;\n}\n")
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	body := readRenderedConf(t, tmp)
	if !hasActiveDirective(body, "map $http_upgrade $connection_upgrade") {
		t.Errorf("servlo's websocket map must survive an operator's own map, got:\n%s", body)
	}
	if strings.Contains(body, "# map $http_upgrade") {
		t.Errorf("servlo's map was retired, leaving its body and brace at http level:\n%s", body)
	}
	if !strings.Contains(body, "include /etc/nginx/conf.d/*.conf;") {
		t.Errorf("the vhost include must survive the filter, got:\n%s", body)
	}
}

// The floor under the next block somebody adds to the template. Whether or not
// the directive is repeatable, a line that opens a block cannot be retired one
// line at a time.
func TestDropOverriddenDefaults_NeverCommentsOutALineThatOpensABlock(t *testing.T) {
	conf := "http {\n    limit_req_zone $binary_remote_addr zone=one:10m rate=1r/s;\n    geo $internal {\n        default 0;\n    }\n}\n"
	got := dropOverriddenDefaults(conf, map[string]bool{"geo": true, "limit_req_zone": true})
	if !hasActiveDirective(got, "geo $internal {") {
		t.Errorf("a block's opening line was commented out, orphaning its body:\n%s", got)
	}
	if hasActiveDirective(got, "limit_req_zone ") {
		t.Errorf("an ordinary overridden directive should still step aside:\n%s", got)
	}
}
