package deploy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// What each deploy did, kept so the next one can undo it.
//
// A record per deploy, appended. The reason this exists before anything
// displays it is that "redeploy the previous commit" has to know which commit
// that was, and the answer is not HEAD~1: a deploy that pulled five commits
// moved the site five commits, and HEAD~1 is a commit nobody ever ran. What the
// operator wants back is where the site was standing before the deploy, which
// is only knowable if the deploy wrote it down.

// Entry is one deploy.
type Entry struct {
	At   time.Time `json:"at"`
	From string    `json:"from"`
	To   string    `json:"to"`
	OK   bool      `json:"ok"`
	// Error is why it failed, empty on success.
	Error string `json:"error,omitempty"`
	// Snapshot names the pre-deploy database backup, empty when the deploy was
	// not schema-changing.
	Snapshot string `json:"snapshot,omitempty"`
	// Kept counts files the exclude list put back.
	Kept       int   `json:"kept,omitempty"`
	DurationMS int64 `json:"duration_ms,omitempty"`
	// Redeploy marks a deploy that went back to an earlier commit rather than
	// forward to a new one.
	Redeploy bool `json:"redeploy,omitempty"`

	// Author and Subject describe the commit that was deployed. A SHA alone
	// does not tell an operator scanning a list which change went out, and by
	// the time they are reading the history the commit may no longer be
	// checked out to look up.
	Author  string `json:"author,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Actor is the panel user who triggered it, empty for a deploy that did not
	// come from a signed-in session.
	Actor string `json:"actor,omitempty"`
}

// HistoryLimit is how many deploys a site keeps.
//
// Bounded because nothing else prunes this file, and a site deploying from a
// webhook on every push writes an entry per push forever. Two hundred is far
// more than anyone scrolls and still a small file.
const HistoryLimit = 200

// HistoryDir holds one file per site.
func HistoryDir() string {
	return filepath.Join(config.DataDir(), "deploy-history")
}

// HistoryPath is where a site's deploys are recorded.
func HistoryPath(site string) (string, error) {
	if !siteFileName.MatchString(site) {
		return "", fmt.Errorf("%q is not a usable site name", site)
	}
	return filepath.Join(HistoryDir(), site+".jsonl"), nil
}

// Record appends one deploy to a site's history.
//
// Append-only, one JSON object per line: a deploy is a fact about something
// that already happened, and rewriting the file would mean a crash mid-write
// could lose the earlier ones too.
func Record(site *config.Site, e Entry) error {
	path, err := HistoryPath(site.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return trimHistory(path)
}

// trimHistory drops the oldest entries once the file is well past the limit.
//
// Rewritten in one go through a temporary file and a rename, so a crash part
// way through leaves the previous history rather than a truncated one. Only
// done when the file has drifted a good way over, because rewriting on every
// single deploy to remove one line is work nobody asked for.
func trimHistory(path string) error {
	lines, err := readLines(path)
	if err != nil || len(lines) <= HistoryLimit+50 {
		return nil
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	for _, l := range lines[len(lines)-HistoryLimit:] {
		if _, err := f.WriteString(l + "\n"); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// readLines returns the file's non-empty lines, oldest first.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		if line := s.Text(); line != "" {
			out = append(out, line)
		}
	}
	return out, s.Err()
}

// History returns a site's deploys, newest first.
//
// A line that will not parse is skipped rather than failing the read. This file
// is a log, and one corrupt line from a half-finished write should not take the
// rest of the history with it.
func History(site *config.Site) ([]Entry, error) {
	path, err := HistoryPath(site.Name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Entry
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		var e Entry
		if json.Unmarshal(s.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// Capped here as well as on disk. The file is trimmed lazily, in batches,
	// so between trims it holds a little more than the limit; a reader should
	// still never be handed more than the limit promises.
	if len(out) > HistoryLimit {
		out = out[:HistoryLimit]
	}
	return out, nil
}

// PreviousCommit is the commit this site was on before its last successful
// deploy, and ok false when there is no deploy to go back from.
//
// Read from the last deploy that worked, not simply the last one. A failed
// deploy that never moved the tree is not something to go back from, and going
// back to its From would land on the same commit the site is already running.
func PreviousCommit(site *config.Site) (string, bool) {
	entries, err := History(site)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.OK || e.From == "" || e.From == e.To {
			continue
		}
		return e.From, true
	}
	return "", false
}
