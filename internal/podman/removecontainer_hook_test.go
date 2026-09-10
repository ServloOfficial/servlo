package podman

import (
	"os/exec"
	"testing"
)

// The panel streams a container's logs with `podman logs -f` and removes the
// same container with `podman rm -f`, from the same process. The two racing over
// one container jam podman's connection pool, so whatever is holding a stream
// open has to be told before the removal, not after it.
func TestRemoveContainer_RunsTheBeforeRemoveHookBeforeRemoving(t *testing.T) {
	var order []string

	prevExec := execCommand
	t.Cleanup(func() { execCommand = prevExec })
	execCommand = func(_ string, args ...string) *exec.Cmd {
		order = append(order, "rm "+args[len(args)-1])
		return exec.Command("true")
	}

	prevHook := BeforeRemove
	t.Cleanup(func() { BeforeRemove = prevHook })
	BeforeRemove = func(name string) { order = append(order, "hook "+name) }

	RemoveContainer("servlo-mysql")

	if len(order) != 2 || order[0] != "hook servlo-mysql" || order[1] != "rm servlo-mysql" {
		t.Errorf("wanted the hook then the removal, got %v", order)
	}
}

// No hook set is the ordinary case: every process that is not the panel holds no
// log streams and sets none.
func TestRemoveContainer_WithNoHookStillRemoves(t *testing.T) {
	var removed string
	prevExec := execCommand
	t.Cleanup(func() { execCommand = prevExec })
	execCommand = func(_ string, args ...string) *exec.Cmd {
		removed = args[len(args)-1]
		return exec.Command("true")
	}

	prevHook := BeforeRemove
	t.Cleanup(func() { BeforeRemove = prevHook })
	BeforeRemove = nil

	RemoveContainer("servlo-redis")
	if removed != "servlo-redis" {
		t.Errorf("removed %q, want servlo-redis", removed)
	}
}
