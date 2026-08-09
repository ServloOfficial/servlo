package dbexec

import "testing"

// Nothing that is not an identifier reaches a statement. The names come from
// site handles and from a file on disk, and both are places a mistake can land.
func TestExpand_RefusesValuesThatCouldBreakTheStatement(t *testing.T) {
	for _, bad := range []string{"acme'; DROP USER root; --", "acme`", "acme user", ""} {
		if _, err := Expand("CREATE USER '{{name}}'", map[string]string{"name": bad}); err == nil {
			t.Errorf("name %q must be refused", bad)
		}
	}
	if _, err := Expand("x {{host}}", map[string]string{"host": "db.example.com; rm -rf /"}); err == nil {
		t.Error("a host with a shell metacharacter must be refused")
	}
}
