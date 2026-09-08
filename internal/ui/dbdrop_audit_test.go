package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/auditlog"
	"github.com/ServloOfficial/servlo/internal/authz"
)

// Dropping a database has to say which database.
//
// The audit log is the file that answers "who deleted the client's data". The
// middleware fills in the actor, the address, the verb and the result from what
// it can see, and what it can see is the method and the path. A database name
// arrives in the body, and auditSubject only reads a site out of a path, so
// without the handler naming it the entry says somebody dropped a database on
// mysql and never which one.
//
// Recorded from the name rather than from the outcome, so a refused attempt is
// in the log too. "alice tried to drop acme_production" is the line an
// investigation wants most.
func TestDatabaseDrop_NamesTheDatabaseInTheAudit(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	guard := &authz.Guard{}
	handler := guard.Audit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleDatabaseDrop(w, r, "mysql")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/databases/mysql/drop",
		strings.NewReader(`{"name":"acme_production"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d audit entries, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Detail, "acme_production") {
		t.Errorf("the audit entry does not name the database that was dropped: %q", entries[0].Detail)
	}
	if !strings.Contains(entries[0].Detail, "mysql") {
		t.Errorf("the audit entry does not name the service it was dropped from: %q", entries[0].Detail)
	}
}
