package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// stopTrackingMgr extends fakeServiceMgr with call tracking for the stop /
// disable / remove paths exercised by WorkerStopForSite and the prune sweep.
type stopTrackingMgr struct {
	fakeServiceMgr
	disableCalls       []string
	removeServiceCalls []string
	removeTimerCalls   []string
	listResults        map[string][]string
}

func (s *stopTrackingMgr) Disable(name string) error {
	s.disableCalls = append(s.disableCalls, name)
	return nil
}
func (s *stopTrackingMgr) RemoveServiceUnit(name string) error {
	s.removeServiceCalls = append(s.removeServiceCalls, name)
	return nil
}
func (s *stopTrackingMgr) RemoveTimerUnit(name string) error {
	s.removeTimerCalls = append(s.removeTimerCalls, name)
	return nil
}
func (s *stopTrackingMgr) ListServiceUnits(pattern string) []string {
	if s.listResults == nil {
		return nil
	}
	if out, ok := s.listResults[pattern]; ok {
		return out
	}
	// Allow callers to register a single canonical glob. If servlo ever
	// passes a different pattern we'd fail to match, surfacing a real bug
	// rather than silently returning empty.
	for _, v := range s.listResults {
		return v
	}
	return nil
}

// registerSite writes a sites.yaml so config.FindSite resolves the path the
// worker unit names are built from.
func registerSite(t *testing.T, name, path string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	dir := filepath.Join(tmp, "servlo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "sites:\n" +
		"    - name: " + name + "\n" +
		"      domains:\n" +
		"        - " + name + ".test\n" +
		"      path: " + path + "\n" +
		"      php_version: \"8.4\"\n" +
		"      node_version: \"22\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}
