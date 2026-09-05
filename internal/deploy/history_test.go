package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func historyHome(t *testing.T) *config.Site {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: t.TempDir()}
}

func TestHistory_NewestFirst(t *testing.T) {
	site := historyHome(t)
	for _, e := range []Entry{
		{From: "aaa", To: "bbb", OK: true},
		{From: "bbb", To: "ccc", OK: true},
		{From: "ccc", To: "ddd", OK: true},
	} {
		if err := Record(site, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := History(site)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].To != "ddd" || got[2].To != "bbb" {
		t.Errorf("history is not newest first: %v", got)
	}
}

// A site that never deployed has no history, and that is not an error: it is
// what every site looks like before its first deploy.
func TestHistory_MissingIsEmptyNotAnError(t *testing.T) {
	site := historyHome(t)

	got, err := History(site)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

// The history is a log, and one truncated line from a crashed write should not
// take the rest of it with it.
func TestHistory_SkipsACorruptLine(t *testing.T) {
	site := historyHome(t)
	if err := Record(site, Entry{From: "aaa", To: "bbb", OK: true}); err != nil {
		t.Fatal(err)
	}
	path, _ := HistoryPath(site.Name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"from":"bbb","to":` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := Record(site, Entry{From: "ccc", To: "ddd", OK: true}); err != nil {
		t.Fatal(err)
	}

	got, err := History(site)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d entries, want the 2 readable ones: %v", len(got), got)
	}
}

// The whole reason the history exists this early: HEAD~1 is not the answer. A
// deploy that pulled several commits moved the site several commits, and the
// one to go back to is where it was standing, not one step back.
func TestPreviousCommit_IsWhereTheSiteWasNotOneStepBack(t *testing.T) {
	site := historyHome(t)
	if err := Record(site, Entry{From: "aaaaaaa1", To: "eeeeeee5", OK: true}); err != nil {
		t.Fatal(err)
	}

	got, ok := PreviousCommit(site)
	if !ok {
		t.Fatal("no previous commit after a successful deploy")
	}
	if got != "aaaaaaa1" {
		t.Errorf("PreviousCommit = %q, want the commit the site was on", got)
	}
}

// A failed deploy is not something to go back from. It never moved the tree, so
// its From is the commit the site is already running.
func TestPreviousCommit_IgnoresFailedAndStandingStillDeploys(t *testing.T) {
	site := historyHome(t)
	for _, e := range []Entry{
		{From: "aaaaaaa1", To: "bbbbbbb2", OK: true},
		{From: "bbbbbbb2", To: "bbbbbbb2", OK: true}, // pulled nothing
		{From: "bbbbbbb2", To: "ccccccc3", OK: false, Error: "the script failed"},
	} {
		if err := Record(site, e); err != nil {
			t.Fatal(err)
		}
	}

	got, ok := PreviousCommit(site)
	if !ok {
		t.Fatal("no previous commit")
	}
	if got != "aaaaaaa1" {
		t.Errorf("PreviousCommit = %q, want the last deploy that actually moved the site", got)
	}
}

func TestPreviousCommit_NothingToGoBackFrom(t *testing.T) {
	site := historyHome(t)

	if _, ok := PreviousCommit(site); ok {
		t.Error("a site that never deployed offered a commit to go back to")
	}
}

// The name decides the path, so one that could climb out of the directory is
// refused rather than joined into one.
func TestHistoryPath_RefusesAnUnusableSiteName(t *testing.T) {
	historyHome(t)
	for _, bad := range []string{"../etc", "a/b", "", ".", "with space"} {
		if _, err := HistoryPath(bad); err == nil {
			t.Errorf("site name %q was accepted", bad)
		}
	}
	got, err := HistoryPath("shop")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "shop.jsonl" {
		t.Errorf("HistoryPath = %q", got)
	}
}

// The history records where a site has been deployed from, which is a list of
// what runs on this server. Nobody else on the box needs to read it.
func TestRecord_FileIsNotWorldReadable(t *testing.T) {
	site := historyHome(t)
	if err := Record(site, Entry{From: "aaa", To: "bbb", OK: true}); err != nil {
		t.Fatal(err)
	}

	path, _ := HistoryPath(site.Name)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want 0600", perm)
	}
}

// The history answers "who and what", not only "when". A commit SHA on its own
// does not tell an operator scanning a list which change went out.
func TestHistory_CarriesAuthorAndWhoTriggeredIt(t *testing.T) {
	site := historyHome(t)
	if err := Record(site, Entry{
		From: "aaaa1111", To: "bbbb2222", OK: true,
		Author: "Sam Rivera", Subject: "add the orders index", Actor: "alice",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := History(site)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries", len(got))
	}
	e := got[0]
	if e.Author != "Sam Rivera" || e.Subject != "add the orders index" {
		t.Errorf("entry = %+v, does not say what was deployed or by whom it was written", e)
	}
	if e.Actor != "alice" {
		t.Errorf("entry = %+v, does not say who triggered it", e)
	}
}

// A history that grows without bound on a site deploying from a webhook is a
// file nobody prunes, so the log keeps the recent entries and drops the rest.
func TestRecord_KeepsTheHistoryBounded(t *testing.T) {
	site := historyHome(t)
	for i := 0; i < HistoryLimit+25; i++ {
		if err := Record(site, Entry{From: "aaa", To: "bbb", OK: true, Subject: string(rune('a' + i%26))}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := History(site)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > HistoryLimit {
		t.Errorf("kept %d entries, want at most %d", len(got), HistoryLimit)
	}
	// And it is the recent ones that survive, not the first ones.
	if len(got) == 0 || got[0].Subject != string(rune('a'+(HistoryLimit+24)%26)) {
		t.Errorf("the newest entry is not at the front: %+v", got[0])
	}
}
