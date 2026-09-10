package ui

import (
	"context"
	"testing"

	"github.com/ServloOfficial/servlo/internal/podman"
)

// The registry was built so a log stream could be closed before the container
// under it is removed, and its comment says so. Nothing called it: the cancel
// end had no callers anywhere in the tree, and the register end was gated to
// unit names the panel never asks this handler for. So a service the operator
// removed or migrated while its log pane was open left `podman logs -f` and
// `podman rm -f` racing over the same container.
//
// The removal itself is now what closes them, wherever it is issued from.
func TestRemovingAContainerCancelsItsOpenLogStreams(t *testing.T) {
	if podman.BeforeRemove == nil {
		t.Fatal("the panel does not close its log streams before a container is removed")
	}

	watched, cancelWatched := context.WithCancel(context.Background())
	defer cancelWatched()
	other, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()

	defer logStreams.Register("servlo-mysql", cancelWatched)()
	defer logStreams.Register("servlo-redis", cancelOther)()

	podman.BeforeRemove("servlo-mysql")

	if watched.Err() == nil {
		t.Error("the stream on the container being removed was left open")
	}
	if other.Err() != nil {
		t.Error("a stream on a different container was cancelled too")
	}
}
