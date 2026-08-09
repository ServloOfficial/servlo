package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Policy is how much history to keep, in the usual three periods.
//
// Zero for a period means keep none of it, and a policy that is entirely zero
// means keep everything. Those are not the same answer and reading one as the
// other in either direction is how a retention sweep either does nothing
// forever or empties the directory the first time a field is missing.
type Policy struct {
	Daily   int `yaml:"daily,omitempty" json:"daily"`
	Weekly  int `yaml:"weekly,omitempty" json:"weekly"`
	Monthly int `yaml:"monthly,omitempty" json:"monthly"`
}

// Empty reports a policy that asks for nothing, which means no sweeping.
func (p Policy) Empty() bool { return p.Daily == 0 && p.Weekly == 0 && p.Monthly == 0 }

// DefaultPolicy is what a site gets when nobody has said otherwise: a week of
// dailies to recover from a bad deploy, a month of weeklies to recover from
// something noticed late, and a quarter of monthlies because that is roughly
// how long it takes to discover a slow corruption.
var DefaultPolicy = Policy{Daily: 7, Weekly: 4, Monthly: 3}

// Prune thins one site's archives down to the policy and returns how many it
// removed.
//
// The rule is the ordinary grandfather-father-son one: keep the newest archive
// of each of the last N days, then of each of the last N weeks, then of each of
// the last N months. An archive kept by more than one period is kept once.
func Prune(dir, site string, policy Policy, now time.Time) (int, error) {
	if policy.Empty() {
		return 0, nil
	}
	archives, err := listArchives(dir, site)
	if err != nil {
		return 0, err
	}

	keep := map[string]bool{}
	for _, period := range []struct {
		count  int
		bucket func(time.Time) string
	}{
		{policy.Daily, func(t time.Time) string { return t.Format("2006-01-02") }},
		{policy.Weekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%d-W%02d", y, w) }},
		{policy.Monthly, func(t time.Time) string { return t.Format("2006-01") }},
	} {
		markNewestPerBucket(archives, period.count, period.bucket, keep)
	}

	var removed int
	for _, a := range archives {
		if keep[a.path] {
			continue
		}
		if err := os.Remove(a.path); err != nil {
			return removed, fmt.Errorf("removing %s: %w", filepath.Base(a.path), err)
		}
		removed++
	}
	return removed, nil
}

// markNewestPerBucket keeps the newest archive in each of the most recent count
// buckets. Walking newest-first means the first archive seen in a bucket is the
// one to keep, and the buckets fill in the order they should be counted.
func markNewestPerBucket(archives []archive, count int, bucket func(time.Time) string, keep map[string]bool) {
	if count <= 0 {
		return
	}
	seen := map[string]bool{}
	for _, a := range archives {
		b := bucket(a.taken)
		if seen[b] {
			continue
		}
		if len(seen) >= count {
			return
		}
		seen[b] = true
		keep[a.path] = true
	}
}

type archive struct {
	path  string
	taken time.Time
}

// listArchives finds this site's archives, newest first.
//
// The site is matched on the whole slug plus the separator the name uses, not
// on a bare prefix: "acme" is a prefix of "acme_two", and sweeping one must not
// take the other's backups. The time comes from the name rather than from the
// file's mtime, because copying a directory rewrites mtimes and would reshuffle
// a history that is supposed to be stable.
func listArchives(dir, site string) ([]archive, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := site + "-"
	var out []archive
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, Extension) || !strings.HasPrefix(name, prefix) {
			continue
		}
		stamp := strings.TrimSuffix(name[len(prefix):], Extension)
		taken, err := parseStamp(stamp)
		if err != nil {
			// Not one of ours, or renamed by hand. Left alone rather than
			// guessed at: deleting a file servlo cannot read is not its call.
			continue
		}
		out = append(out, archive{path: filepath.Join(dir, name), taken: taken})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].taken.After(out[j].taken) })
	return out, nil
}

// parseStamp reads the time out of an archive name, tolerating the -2 suffix a
// second backup in the same second gets.
func parseStamp(stamp string) (time.Time, error) {
	if i := strings.LastIndex(stamp, "-"); i > 0 && len(stamp) > i+1 {
		if _, err := time.Parse("20060102-150405", stamp); err != nil {
			stamp = stamp[:i]
		}
	}
	return time.Parse("20060102-150405", stamp)
}

// Newest is the most recent archive for a site, which is the one a scheduled
// check should be checking: an older one failing matters, but the one taken
// last night is the one that would actually be used.
func Newest(dir, site string) (string, error) {
	archives, err := listArchives(dir, site)
	if err != nil {
		return "", err
	}
	if len(archives) == 0 {
		return "", fmt.Errorf("no backups of %s in %s yet", site, dir)
	}
	return archives[0].path, nil
}

// Entry is one archive, as a list shows it.
type Entry struct {
	Name  string    `json:"name"`
	Size  int64     `json:"size"`
	Taken time.Time `json:"taken"`
}

// List is a site's archives, newest first, for a panel that shows what is
// actually recoverable rather than what was scheduled.
func List(dir, site string) ([]Entry, error) {
	archives, err := listArchives(dir, site)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(archives))
	for _, a := range archives {
		info, err := os.Stat(a.path)
		if err != nil {
			continue
		}
		out = append(out, Entry{Name: filepath.Base(a.path), Size: info.Size(), Taken: a.taken})
	}
	return out, nil
}
