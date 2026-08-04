package systemd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTimerIfChanged(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	const content = "[Unit]\nDescription=Test\n\n[Timer]\nOnCalendar=minutely\n"

	changed, err := WriteTimerIfChanged("servlo-test", content)
	if err != nil {
		t.Fatalf("WriteTimerIfChanged: %v", err)
	}
	if !changed {
		t.Errorf("first write reported changed=false, want true")
	}

	path := filepath.Join(tmp, "systemd", "user", "servlo-test.timer")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != content {
		t.Errorf("on-disk content = %q, want %q", got, content)
	}

	// Second write with identical content must report unchanged.
	changed, err = WriteTimerIfChanged("servlo-test", content)
	if err != nil {
		t.Fatalf("second WriteTimerIfChanged: %v", err)
	}
	if changed {
		t.Errorf("idempotent write reported changed=true, want false")
	}
}

func TestRemoveTimer(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if _, err := WriteTimerIfChanged("servlo-test", "[Timer]\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := RemoveTimer("servlo-test"); err != nil {
		t.Fatalf("RemoveTimer: %v", err)
	}
	path := filepath.Join(tmp, "systemd", "user", "servlo-test.timer")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still present after RemoveTimer: %v", err)
	}

	// Removing a missing file must not error.
	if err := RemoveTimer("servlo-nonexistent"); err != nil {
		t.Errorf("RemoveTimer of missing file: %v", err)
	}
}
