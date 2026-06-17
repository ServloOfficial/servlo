//go:build linux

package logsource

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// readJournal reads a unit's logs from the systemd user journal. Time and grep
// filters push down to journalctl; the cursor is the journal's own opaque
// __CURSOR so the next poll resumes exactly with --after-cursor.
func readJournal(src Source, opts Opts) (Result, error) {
	args := []string{"--user", "-u", src.Locator + ".service", "--no-pager", "--output=json"}
	if opts.Since != "" {
		if isJournalCursor(opts.Since) {
			args = append(args, "--after-cursor="+opts.Since)
		} else {
			args = append(args, "--since", journalTime(opts.Since))
		}
	}
	if opts.Until != "" {
		args = append(args, "--until", journalTime(opts.Until))
	}
	// journalctl -g takes a regex; an invalid one makes it error out and we'd
	// silently return nothing. When the pattern won't compile, drop the push-down
	// and filter in-process with the shared literal fallback, matching the file
	// and podman paths so the same grep input behaves the same on every source.
	var fallback func(string) bool
	if opts.Grep != "" {
		if _, err := regexp.Compile(opts.Grep); err == nil {
			args = append(args, "-g", opts.Grep)
		} else {
			fallback = compileGrep(opts.Grep)
		}
	}
	args = append(args, "-n", strconv.Itoa(opts.Lines))

	var buf bytes.Buffer
	cmd := exec.Command("journalctl", args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run() // missing unit / no journal access — return what we have

	var out []Entry
	var cursor string
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for {
		var je journalEntry
		if err := dec.Decode(&je); err != nil {
			break
		}
		// Advance the cursor past every decoded entry, including ones the literal
		// fallback drops, so the next poll resumes after them rather than re-scanning.
		if je.Cursor != "" {
			cursor = je.Cursor
		}
		text := je.message()
		if fallback != nil && !fallback(text) {
			continue
		}
		out = append(out, Entry{Time: je.timeString(), Text: text})
	}
	return Result{Entries: out, Cursor: cursor}, nil
}

func isJournalCursor(s string) bool {
	return strings.HasPrefix(s, "s=") && strings.Contains(s, ";i=")
}

// journalTime converts a since/until value to journalctl's accepted forms:
// relative Go durations become "-15m"; absolute times become "YYYY-MM-DD HH:MM:SS".
func journalTime(s string) string {
	s = strings.TrimSpace(s)
	if _, err := time.ParseDuration(s); err == nil {
		return "-" + s
	}
	if t, ok := parseAbs(s); ok {
		// journalctl reads a zone-less time in the system's local zone, so emit
		// the local wall-clock of the parsed instant.
		return t.In(time.Local).Format("2006-01-02 15:04:05")
	}
	return s
}

type journalEntry struct {
	Cursor   string          `json:"__CURSOR"`
	Realtime string          `json:"__REALTIME_TIMESTAMP"`
	Message  json.RawMessage `json:"MESSAGE"`
}

func (j journalEntry) message() string {
	if len(j.Message) == 0 {
		return ""
	}
	if j.Message[0] == '"' {
		var s string
		_ = json.Unmarshal(j.Message, &s)
		return s
	}
	// journalctl encodes binary messages as a JSON array of bytes.
	var b []byte
	if json.Unmarshal(j.Message, &b) == nil {
		return string(b)
	}
	return ""
}

func (j journalEntry) timeString() string {
	if j.Realtime == "" {
		return ""
	}
	usec, err := strconv.ParseInt(j.Realtime, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(0, usec*1000).Format(time.RFC3339)
}
