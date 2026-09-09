package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Every write to the registry is a read-modify-write, and the file write itself
// is atomic, so two of them running at once do not corrupt sites.yaml: the last
// full write simply wins and the other site's change is gone, with nothing
// returning an error about it. One lock has to span the whole sequence, and it
// has to be the same lock for every caller, including the ones outside this
// package that used to load, mutate and save on their own.
func TestUpdateSites_ConcurrentWritersDoNotLoseEachOther(t *testing.T) {
	const n = 24
	for i := 0; i < n; i++ {
		if err := AddSite(Site{Name: fmt.Sprintf("site%02d", i), Path: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("fw%02d", i)
			errs[i] = UpdateSites(func(reg *SiteRegistry) (bool, error) {
				for j := range reg.Sites {
					if reg.Sites[j].Name == fmt.Sprintf("site%02d", i) {
						reg.Sites[j].Framework = want
						return true, nil
					}
				}
				return false, fmt.Errorf("site%02d went missing from the registry", i)
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range reg.Sites {
		got[s.Name] = s.Framework
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("site%02d", i)
		if want := fmt.Sprintf("fw%02d", i); got[name] != want {
			t.Errorf("%s: framework is %q, and the write that set %q was dropped", name, got[name], want)
		}
	}
}

// A mutation that reports no change must not rewrite the file, so a sweep that
// finds nothing to repair does not churn sites.yaml on every pass.
func TestUpdateSites_NoChangeDoesNotWrite(t *testing.T) {
	if err := AddSite(Site{Name: "quiet", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	before := statSites(t)
	if err := UpdateSites(func(*SiteRegistry) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	if after := statSites(t); after != before {
		t.Errorf("sites.yaml changed (%v -> %v) for a mutation that reported nothing to do", before, after)
	}
}

func statSites(t *testing.T) [2]int64 {
	t.Helper()
	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, s := range reg.Sites {
		sum += int64(len(s.Name) + len(s.Framework))
	}
	return [2]int64{int64(len(reg.Sites)), sum}
}

// SaveSites replaces the whole registry, which is the right call for seeding a
// fixture and the wrong one for changing a field: a caller that loads, mutates
// and saves outside this package holds no lock for the gap between the load and
// the save, so it silently drops whatever anything else wrote in between. Three
// callers did exactly that. UpdateSites is the way in, and this is what keeps a
// fourth from appearing.
func TestSaveSites_IsNotCalledFromOutsideThisPackage(t *testing.T) {
	root := repoRoot(t)
	found := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasPrefix(path, filepath.Join(root, "internal", "config")+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "config.SaveSites(") {
				found++
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d loads, mutates and saves the registry outside its lock: use config.UpdateSites", rel, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = found
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory, so this proves nothing")
		}
		dir = parent
	}
}
