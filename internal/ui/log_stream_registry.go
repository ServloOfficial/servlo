package ui

import (
	"context"
	"sync"

	"github.com/ServloOfficial/servlo/internal/podman"
)

// logStreamRegistry tracks the panel's open log streams by container, so one can
// be closed before something removes the container under it. Without this,
// `podman logs -f` from an open log pane races the `podman rm -f` behind a
// service remove, restart, reinstall or migration, jamming the connection pool
// and eventually wedging the podman API socket.
type logStreamRegistry struct {
	mu      sync.Mutex
	streams map[string]map[*context.CancelFunc]struct{}
}

func newLogStreamRegistry() *logStreamRegistry {
	return &logStreamRegistry{streams: make(map[string]map[*context.CancelFunc]struct{})}
}

// Register adds cancel under unit and returns a deregister func the caller
// must invoke when the stream ends. The cancel func is stored by pointer
// so multiple streams for the same unit can each unregister independently.
func (r *logStreamRegistry) Register(unit string, cancel context.CancelFunc) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	cf := cancel
	cfp := &cf
	if r.streams[unit] == nil {
		r.streams[unit] = make(map[*context.CancelFunc]struct{})
	}
	r.streams[unit][cfp] = struct{}{}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if set, ok := r.streams[unit]; ok {
			delete(set, cfp)
			if len(set) == 0 {
				delete(r.streams, unit)
			}
		}
	}
}

// CancelAllFor cancels every stream registered under any of the named units.
// The cancelled HTTP handler exits, which sends SIGKILL to its `podman logs -f`
// child via exec.CommandContext, releasing the connection.
func (r *logStreamRegistry) CancelAllFor(units []string) {
	r.mu.Lock()
	cfs := make([]context.CancelFunc, 0)
	for _, u := range units {
		for cfp := range r.streams[u] {
			cfs = append(cfs, *cfp)
		}
	}
	r.mu.Unlock()
	for _, c := range cfs {
		c()
	}
}

// logStreams is the process-wide registry of open log streams, so one can be
// cancelled before something removes the container it is reading.
var logStreams = newLogStreamRegistry()

// Every removal goes through podman.RemoveContainer, so hooking it there rather
// than at each of the panel's service actions means a new one cannot forget.
func init() {
	podman.BeforeRemove = func(unit string) { logStreams.CancelAllFor([]string{unit}) }
}
