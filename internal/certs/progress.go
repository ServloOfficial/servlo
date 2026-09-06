package certs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Issuance progress, and why it is on disk rather than in memory.
//
// A certificate is issued inside one synchronous request, and that request can
// take a minute while an authority validates. The panel needs to show what is
// happening during that minute, which it cannot do by waiting for the response
// it is already waiting for. Writing each step to a small per-domain file lets
// the panel poll it on a second request, and means the CLI and the panel see
// the same record of what happened whichever one started the issuance.
//
// It is a log of the last attempt, not a history: each issuance truncates it.
// What an operator needs is why this one failed, and keeping every earlier
// attempt would bury that.

// progressMu serialises writes so two issuances for different domains cannot
// interleave a line, and so a reader never sees a half-written entry.
var progressMu sync.Mutex

func progressPath(domain string) string {
	return filepath.Join(config.CertsDir(), "progress", domain+".log")
}

// startProgress truncates the log for a fresh attempt.
func startProgress(domain string) {
	progressMu.Lock()
	defer progressMu.Unlock()
	path := progressPath(domain)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	os.WriteFile(path, nil, 0644) //nolint:errcheck — progress is best effort
}

// noteProgress appends one step. Failures are ignored on purpose: losing a
// progress line must never fail an issuance that is otherwise working.
func noteProgress(domain, format string, args ...any) {
	progressMu.Lock()
	defer progressMu.Unlock()
	f, err := os.OpenFile(progressPath(domain), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	line := time.Now().UTC().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	fmt.Fprintln(f, line) //nolint:errcheck
}

// Progress returns the steps of the most recent issuance attempt for a domain,
// oldest first. Empty when none has run since this install started.
func Progress(domain string) []string {
	progressMu.Lock()
	defer progressMu.Unlock()
	data, err := os.ReadFile(progressPath(domain))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
