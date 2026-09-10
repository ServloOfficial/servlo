package imgledger

import (
	"os"
	"path/filepath"
	"testing"
)

// The ledger is written by whichever servlo process pulled an image: the panel,
// the watcher, or a CLI run over SSH. The mutex above orders writers inside one
// process and says nothing about the other two.
//
// So the staging file has to be this writer's alone. Staging at a fixed
// <path>.tmp means two processes writing at once share one staging file, and
// what gets renamed into place is whichever bytes landed last over whichever
// landed first. The comment on save says a concurrent reader or a mid-write
// crash can never see a truncated file, which is true of those two and was never
// true of a second writer.
//
// A ledger that cannot be parsed reads as empty, which is the conservative
// direction: cleanup then reaps nothing rather than reaping an image it should
// not. The cost is the other way round, images servlo pulled never being
// reclaimed on a droplet whose disk is the thing filling up.
func TestRecord_DoesNotWriteThroughAnotherWritersStagingFile(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "pulled-images.json")
	orig := pathFn
	pathFn = func() string { return ledger }
	t.Cleanup(func() { pathFn = orig })

	// What a second process staging its own write looks like from here.
	const theirs = "another writer's staging file"
	elsewhere := ledger + ".tmp"
	if err := os.WriteFile(elsewhere, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}

	Record("ghcr.io/servloofficial/servlo-php:8.4")

	got, err := os.ReadFile(elsewhere)
	if err != nil {
		t.Fatalf("the other writer's staging file is gone: %v", err)
	}
	if string(got) != theirs {
		t.Errorf("the other writer's staging file was written through: %q", got)
	}

	// And this writer's own record still landed.
	if !Load()["ghcr.io/servloofficial/servlo-php:8.4"] {
		t.Error("the ref was not recorded")
	}
}
