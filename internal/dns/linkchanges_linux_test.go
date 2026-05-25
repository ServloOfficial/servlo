package dns

import (
	"os/exec"
	"testing"
	"time"
)

// TestLinkChanges_realKernel exercises the rtnetlink subscription against
// the live kernel. It brings up and tears down a dummy interface and
// asserts that linkChanges sees at least one event from each transition.
// Skips when dummy interfaces aren't usable (e.g. CI without CAP_NET_ADMIN
// or the dummy kernel module).
func TestLinkChanges_realKernel(t *testing.T) {
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("ip(8) not available")
	}
	probe := exec.Command("ip", "link", "add", "lerd-nltest0", "type", "dummy")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("cannot create dummy interface (need CAP_NET_ADMIN): %v\n%s", err, out)
	}
	defer exec.Command("ip", "link", "del", "lerd-nltest0").Run()

	out := make(chan struct{}, 16)
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- LinkChanges(out, done) }()
	defer func() {
		close(done)
		if err := <-errCh; err != nil {
			t.Errorf("LinkChanges returned %v after clean shutdown, want nil", err)
		}
	}()

	time.Sleep(150 * time.Millisecond)
	drain(out)

	if err := exec.Command("ip", "link", "set", "lerd-nltest0", "up").Run(); err != nil {
		t.Fatalf("link up: %v", err)
	}
	select {
	case <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("no netlink event on link up")
	}

	drain(out)
	if err := exec.Command("ip", "link", "set", "lerd-nltest0", "down").Run(); err != nil {
		t.Fatalf("link down: %v", err)
	}
	select {
	case <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("no netlink event on link down")
	}
}

func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
