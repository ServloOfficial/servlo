package config

import "testing"

func TestWorktreeLANRegistry_emptyByDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	entries, err := LoadWorktreeLANRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty registry, got %d", len(entries))
	}
}

func TestWorktreeLANRegistry_addFindRemove(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	e := WorktreeLANEntry{Site: "acme", Branch: "feat-a", Port: 9101}
	if err := AddWorktreeLAN(e); err != nil {
		t.Fatal(err)
	}
	got, ok, err := FindWorktreeLAN("acme", "feat-a")
	if err != nil || !ok || got != e {
		t.Fatalf("FindWorktreeLAN got=%+v ok=%v err=%v", got, ok, err)
	}
	removed, ok, err := RemoveWorktreeLAN("acme", "feat-a")
	if err != nil || !ok || removed != e {
		t.Fatalf("RemoveWorktreeLAN got=%+v ok=%v err=%v", removed, ok, err)
	}
	if _, stillThere, _ := FindWorktreeLAN("acme", "feat-a"); stillThere {
		t.Errorf("entry still in registry after Remove")
	}
}

func TestWorktreeLANRegistry_addUpdates(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	_ = AddWorktreeLAN(WorktreeLANEntry{Site: "acme", Branch: "feat-a", Port: 9101})
	_ = AddWorktreeLAN(WorktreeLANEntry{Site: "acme", Branch: "feat-a", Port: 9102})
	entries, _ := LoadWorktreeLANRegistry()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after update, got %d", len(entries))
	}
	if entries[0].Port != 9102 {
		t.Errorf("Port = %d, want 9102", entries[0].Port)
	}
}
