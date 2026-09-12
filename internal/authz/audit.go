package authz

import (
	"context"
	"net/http"
	"strings"

	"github.com/ServloOfficial/servlo/internal/auditlog"
)

// Auditing at the chokepoint.
//
// Seventy handlers is seventy chances to forget, and the one that forgets is
// the one somebody later needs. So the middleware records, and a route added
// tomorrow is audited the day it is added without anyone remembering to.
//
// Reads are not recorded. A log with every page view in it is a log nobody
// reads, and the question an audit answers is what changed.
//
// A refused request is recorded, and it is the interesting one: somebody
// reaching for something they may not have is most of why the log exists.

// auditRecorder captures the status a handler wrote, so the entry can say
// whether the thing being audited actually happened.
type auditRecorder struct {
	http.ResponseWriter
	status int
}

func (a *auditRecorder) WriteHeader(status int) {
	a.status = status
	a.ResponseWriter.WriteHeader(status)
}

func (a *auditRecorder) Write(b []byte) (int, error) {
	if a.status == 0 {
		// A handler that writes without calling WriteHeader has answered 200.
		a.status = http.StatusOK
	}
	return a.ResponseWriter.Write(b)
}

// Unwrap is how a caller reaches the real writer through this wrapper, which
// is what keeps wrapping the response from breaking every route that streams.
// It only helps a caller that asks: http.ResponseController follows it, and a
// type assertion for http.Flusher or http.Hijacker does not, because embedding
// the interface promotes only the three methods it declares. The panel's
// streaming routes go through the controller for that reason.
func (a *auditRecorder) Unwrap() http.ResponseWriter { return a.ResponseWriter }

type ctxKeyAuditDetail struct{}

// auditDetail is the note a handler may leave for the entry about to be
// written. A pointer in the context rather than a new context returned from the
// handler, because a handler does not get to replace the request the middleware
// is holding.
type auditDetail struct{ text string }

// SetAuditDetail records what a request actually acted on, for the audit entry
// the middleware writes when the handler returns.
//
// The method and the path answer "somebody saved a file on acme.com". They do
// not answer "which file", because the path carries the site and the file
// arrives in a body or a query, and the query is deliberately never logged.
// Anything whose subject is finer-grained than the route says so here.
//
// Calling it is optional and calling it twice replaces the note: the entry is
// written either way, so a handler that forgets loses detail rather than the
// record.
func SetAuditDetail(r *http.Request, detail string) {
	if note, ok := r.Context().Value(ctxKeyAuditDetail{}).(*auditDetail); ok {
		note.text = detail
	}
}

// Audit records every state-changing request that passes through it.
func (g *Guard) Audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !stateChanging(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		recorder := &auditRecorder{ResponseWriter: w}
		note := &auditDetail{}
		r = r.WithContext(context.WithValue(r.Context(), ctxKeyAuditDetail{}, note))
		next.ServeHTTP(recorder, r)

		actor := ""
		if session, ok := SessionFrom(r.Context()); ok {
			actor = session.User
		}
		result := auditlog.ResultOK
		if recorder.status >= 400 {
			result = auditlog.ResultFailed
		}
		auditlog.Record(auditlog.Entry{
			Action:  auditAction(r.Method, r.URL.Path),
			Subject: auditSubject(r.URL.Path),
			Actor:   actor,
			IP:      sourceAddress(r),
			Result:  result,
			Detail:  note.text,
		})
	})
}

// auditAction turns a method and a path into a verb an operator can scan.
//
// The path only: never the query string. The log is 0600 and it is also the
// file somebody pastes into a support thread, so a token that arrived in a
// query must not be in it in the first place.
func auditAction(method, path string) string {
	rest := strings.Trim(strings.TrimPrefix(path, "/api"), "/")
	parts := strings.Split(rest, "/")

	// A site action reads as site.<verb>, because which site it was belongs in
	// the subject rather than repeated in the verb.
	if len(parts) >= 2 && parts[0] == "sites" {
		if len(parts) >= 3 {
			return "site." + strings.Join(parts[2:], ".")
		}
		return "site." + methodVerb(method)
	}
	if rest == "" {
		return "api." + methodVerb(method)
	}
	return strings.Join(parts, ".")
}

// auditSubject is what the action happened to: the site, when there is one.
func auditSubject(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/sites/")
	if !ok {
		return ""
	}
	if slash := strings.Index(rest, "/"); slash >= 0 {
		rest = rest[:slash]
	}
	return rest
}

func methodVerb(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}
