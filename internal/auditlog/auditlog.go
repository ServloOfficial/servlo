// Package auditlog is the append-only record of what changed on this machine.
//
// It starts here because S3.5 needs somewhere to record a failed renewal, and
// stays deliberately small: one entry per line, JSON, opened O_APPEND, never
// rewritten. S5.6 widens it to every state-changing route and adds rotation and
// the permission registry around it; nothing here should have to change for
// that, only grow.
//
// The one rule that must not soften: the application appends and never edits.
// A log a process can rewrite is a log that tells you what the last writer
// wanted you to believe.
package auditlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// Entry is one thing that happened.
type Entry struct {
	At time.Time `json:"at"`
	// Action is a dotted verb: cert.renew.failed, site.secure, dnsprovider.set.
	Action string `json:"action"`
	// Subject is what it happened to, usually a domain or a site name.
	Subject string `json:"subject,omitempty"`
	// Actor is who did it. Empty means servlo itself, on a timer or a watcher
	// pass, which is the case for everything until authentication lands.
	Actor string `json:"actor,omitempty"`
	// Detail is free text for a human. Redacted on the way in.
	Detail string `json:"detail,omitempty"`
}

var mu sync.Mutex

// Path is where the log lives.
func Path() string { return filepath.Join(config.DataDir(), "audit.log") }

// secretPattern matches the shapes a secret arrives in when someone puts an
// error message straight into Detail. The log is 0600, but it is also the file
// an operator pastes into a support thread, so a token must not be in it in the
// first place.
var secretPattern = regexp.MustCompile(`(?i)\b(token|password|passwd|secret|api[_-]?key|access[_-]?key|bearer)\b\s*[=:]\s*\S+`)

func redact(s string) string {
	return secretPattern.ReplaceAllStringFunc(s, func(match string) string {
		loc := regexp.MustCompile(`\s*[=:]\s*`).FindStringIndex(match)
		if loc == nil {
			return match
		}
		return match[:loc[1]] + "[redacted]"
	})
}

// Append writes one entry. Opened O_APPEND so concurrent writers from the panel
// and the watcher interleave whole lines rather than overwriting each other,
// and created 0600 so it is never briefly readable.
func Append(e Entry) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	e.Detail = redact(e.Detail)

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// Record appends and swallows the error. For call sites where failing to log
// must not fail the operation being logged, which is most of them: losing an
// audit line is bad, and aborting a certificate renewal because the disk is
// full is worse.
func Record(e Entry) {
	_ = Append(e)
}

// Recent returns up to n entries, newest first. A missing log is no entries
// rather than an error, since nothing having happened yet is a normal state.
//
// A line that will not parse is skipped rather than failing the read: a
// truncated write from a killed process should cost that line and not the
// readable ones around it.
func Recent(n int) ([]Entry, error) {
	f, err := os.Open(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	var all []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Newest first: the question being asked is what just went wrong.
	out := make([]Entry, 0, n)
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out, nil
}
