package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// auditShaped is the shape internal/authz wraps every state-changing response
// in, so the audit entry can say what status the handler wrote: the
// ResponseWriter embedded as an interface, plus Unwrap. Embedding the interface
// promotes only the three methods it declares, so the wrapper is not an
// http.Flusher however flushable the writer underneath it is.
type auditShaped struct {
	http.ResponseWriter
}

func (a auditShaped) Unwrap() http.ResponseWriter { return a.ResponseWriter }

// Deploy, redeploy, and both PHP image builds are POSTs, so every one of them
// arrives through that wrapper, and every one of them answers a browser with
// "streaming not supported" rather than with the thing it was asked to do.
//
// Unwrap is what a caller is supposed to reach the real writer through, and it
// only works for a caller that asks: http.ResponseController consults it, and a
// plain type assertion does not.
func TestStartPHPBuildStream_StreamsThroughTheWrapperEveryPostArrivesIn(t *testing.T) {
	rec := httptest.NewRecorder()

	sw, done, ok := startPHPBuildStream(auditShaped{rec})
	if !ok {
		t.Fatal("a streaming route refused to stream, so the panel answers 500 for every deploy")
	}
	if _, err := sw.Write([]byte("a line of output\n")); err != nil {
		t.Fatal(err)
	}
	done(map[string]any{"ok": true})

	body := rec.Body.String()
	if !strings.Contains(body, "data: a line of output") {
		t.Errorf("the output never reached the stream:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("the stream never finished:\n%s", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want the SSE one", got)
	}
}

// And an unwrapped writer keeps working, which is the path the unix socket
// takes when nothing wrapped the response at all.
func TestStartPHPBuildStream_StillStreamsUnwrapped(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, _, ok := startPHPBuildStream(rec); !ok {
		t.Fatal("a bare ResponseWriter stopped being able to stream")
	}
}

// The standing rule, because the mistake is invisible: a type assertion
// compiles, runs, and simply reports that the writer cannot stream, and the
// route answers 500 from then on. http.ResponseController is the one way to
// reach a wrapped writer, and it is the only one allowed here.
func TestNoRouteAssertsItsWayToTheWriter(t *testing.T) {
	banned := []string{"w.(http.Flusher)", "w.(http.Hijacker)", "w.(http.CloseNotifier)"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range banned {
			if strings.Contains(string(body), bad) {
				t.Errorf("%s asserts %s, which stops at the audit middleware's wrapper: use http.NewResponseController(w)", name, bad)
			}
		}
	}
}
