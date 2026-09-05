package authz

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/auditlog"
)

func auditedHandler(t *testing.T, guard *Guard, status int) http.Handler {
	t.Helper()
	// Require establishes who, Audit records, ScopeSites decides. Audit sits
	// outside the decision so it sees a refusal, which is the entry somebody
	// is most often looking for.
	return guard.Require(guard.Audit(guard.ScopeSites(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))))
}

// Recording at the chokepoint rather than in each handler is the whole point:
// seventy handlers is seventy chances to forget, and the one that forgets is
// the one somebody later needs.
func TestAudit_RecordsEveryStateChangingRequest(t *testing.T) {
	guard := scopedGuard(t)
	handler := auditedHandler(t, guard, http.StatusOK)

	handler.ServeHTTP(httptest.NewRecorder(), signedInAs(t, guard, "alice", http.MethodPost, "/api/sites/example.com/deploy"))

	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1: %+v", len(entries), entries)
	}
	entry := entries[0]
	// The five questions asked of an audit line after something goes wrong.
	if entry.At.IsZero() {
		t.Error("no timestamp")
	}
	if entry.Actor != "alice" {
		t.Errorf("actor = %q, want alice", entry.Actor)
	}
	if entry.IP != "203.0.113.9" {
		t.Errorf("ip = %q, want the source address", entry.IP)
	}
	if !strings.Contains(entry.Action, "deploy") {
		t.Errorf("action = %q, does not name what happened", entry.Action)
	}
	if entry.Subject != "example.com" {
		t.Errorf("subject = %q, want the site it happened to", entry.Subject)
	}
	if entry.Result != auditlog.ResultOK {
		t.Errorf("result = %q, want ok", entry.Result)
	}
}

// A log of attempts that does not say which ones worked describes half of what
// happened, and the failures are the half somebody is usually looking for.
func TestAudit_RecordsFailures(t *testing.T) {
	guard := scopedGuard(t)
	handler := auditedHandler(t, guard, http.StatusInternalServerError)

	handler.ServeHTTP(httptest.NewRecorder(), signedInAs(t, guard, "alice", http.MethodPost, "/api/sites/example.com/deploy"))

	entries, _ := auditlog.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(entries))
	}
	if entries[0].Result != auditlog.ResultFailed {
		t.Errorf("result = %q, want failed", entries[0].Result)
	}
}

// Reads are not recorded. A log with every page view in it is a log nobody
// reads, and the question an audit answers is what changed.
func TestAudit_DoesNotRecordReads(t *testing.T) {
	guard := scopedGuard(t)
	handler := auditedHandler(t, guard, http.StatusOK)

	handler.ServeHTTP(httptest.NewRecorder(), signedInAs(t, guard, "alice", http.MethodGet, "/api/sites"))

	entries, _ := auditlog.Recent(10)
	if len(entries) != 0 {
		t.Errorf("a read was recorded: %+v", entries)
	}
}

// A refused request is recorded too, and that is the interesting one: somebody
// reaching for something they may not have is what an audit log is for.
func TestAudit_RecordsARefusal(t *testing.T) {
	guard := scopedGuard(t)
	var reached bool
	handler := guard.Require(guard.Audit(guard.ScopeSites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))))

	handler.ServeHTTP(httptest.NewRecorder(), signedInAs(t, guard, "dev", http.MethodPost, "/api/sites/other.com/deploy"))

	if reached {
		t.Fatal("the request reached the handler")
	}
	entries, _ := auditlog.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("a refused request recorded %d entries, want 1", len(entries))
	}
	if entries[0].Result != auditlog.ResultFailed {
		t.Errorf("result = %q, want failed", entries[0].Result)
	}
	if entries[0].Actor != "dev" {
		t.Errorf("actor = %q, want dev", entries[0].Actor)
	}
}

// The action reads as a verb rather than a URL, because an operator scanning
// the log is asking what happened, not which endpoint served it.
func TestAudit_ActionNamesTheChange(t *testing.T) {
	for _, tc := range []struct {
		method, path, want string
	}{
		{http.MethodPost, "/api/sites/example.com/deploy", "site.deploy"},
		{http.MethodDelete, "/api/sites/example.com", "site.delete"},
		{http.MethodPost, "/api/services/mysql/start", "services.mysql.start"},
		{http.MethodPost, "/api/servlo/stop", "servlo.stop"},
	} {
		if got := auditAction(tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// The log is 0600 and also the file an operator pastes into a support thread,
// so a query string carrying a secret must not reach it.
func TestAudit_DoesNotRecordQueryStrings(t *testing.T) {
	guard := scopedGuard(t)
	handler := auditedHandler(t, guard, http.StatusOK)

	req := signedInAs(t, guard, "alice", http.MethodPost, "/api/sites/example.com/deploy?token=supersecret")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	entries, _ := auditlog.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries", len(entries))
	}
	for _, field := range []string{entries[0].Action, entries[0].Subject, entries[0].Detail} {
		if strings.Contains(field, "supersecret") {
			t.Errorf("the audit entry carries the query string: %q", field)
		}
	}
}

// The route says a file was saved on a site. It cannot say which file, because
// the name arrives in a body or a query and the query is never logged. A
// handler that knows fills that in, and the entry carries it.
func TestAudit_RecordsTheDetailAHandlerLeaves(t *testing.T) {
	guard := scopedGuard(t)
	handler := guard.Require(guard.Audit(guard.ScopeSites(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetAuditDetail(r, "saved wp-config.php")
		w.WriteHeader(http.StatusOK)
	}))))

	handler.ServeHTTP(httptest.NewRecorder(), signedInAs(t, guard, "alice", http.MethodPut, "/api/sites/example.com/files/content"))

	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(entries))
	}
	if entries[0].Detail != "saved wp-config.php" {
		t.Errorf("detail = %q, want the file the handler named", entries[0].Detail)
	}
}

// A handler reached outside the audit middleware must not panic when it leaves
// a note, because the note is for an entry nobody is writing.
func TestSetAuditDetail_IsANoOpWithoutTheMiddleware(t *testing.T) {
	SetAuditDetail(httptest.NewRequest(http.MethodPost, "/api/sites/example.com/files/content", nil), "nothing is listening")
}
