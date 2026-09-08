package appinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/nginx"
)

// The bug this guards: servlo answers a domain no site is linked to with its
// own branded page, so the catch-all satisfied a wait that only asked whether
// anything answered, and the installer was driven against nginx's placeholder.
func TestWaitForSiteIgnoresTheCatchAll(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&hits, 1) <= 3 {
			w.Header().Set(nginx.CatchAllHeader, nginx.CatchAllHeaderValue)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := waitForSite(context.Background(), srv.URL); err != nil {
		t.Fatalf("waitForSite: %v", err)
	}
	if got := atomic.LoadInt64(&hits); got != 4 {
		t.Errorf("expected the wait to keep polling past the catch-all, got %d requests", got)
	}
}

// A site that is up but has nothing to say for itself is still up. Judging the
// body would mean knowing what each application looks like before its own
// installer has run.
func TestWaitForSiteAcceptsAnErrorFromTheSiteItself(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "File not found.", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := waitForSite(context.Background(), srv.URL); err != nil {
		t.Fatalf("waitForSite: %v", err)
	}
}

// A 502 is nginx saying the site's pool has no worker on the socket yet. That
// is the second half of the same window, and it is never something a freshly
// unpacked application chose to answer.
func TestWaitForSiteWaitsOutAGatewayError(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&hits, 1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := waitForSite(context.Background(), srv.URL); err != nil {
		t.Fatalf("waitForSite: %v", err)
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("expected the wait to keep polling past the 502, got %d requests", got)
	}
}

func TestWaitForSiteGivesUpOnAPermanentCatchAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(nginx.CatchAllHeader, nginx.CatchAllHeaderValue)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	err := waitForSite(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected the wait to fail rather than hand the catch-all to the installer")
	}
	if !strings.Contains(err.Error(), "catch-all") && err != context.DeadlineExceeded {
		t.Errorf("expected the failure to name the catch-all, got %q", err)
	}
}
