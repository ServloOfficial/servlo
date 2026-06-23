package cli

import "testing"

// A present-but-empty .lerd.yaml reached through `lerd link` must still run the
// wizard. runLinkOrInit routes on content (IsEmpty), but runInit decides on
// file existence, so it must be forced with fresh=true. This pins the chain:
// empty config -> linkShouldRunWizard=true -> runInit(fresh=true) ->
// initShouldRunWizard=true.
func TestInitShouldRunWizard_EmptyPresentConfigForcedByLinkPath(t *testing.T) {
	// The link path passes fresh=true once it has decided to run the wizard.
	const linkPathFresh = true

	// File present (empty), link path forces fresh -> wizard runs.
	if !initShouldRunWizard(true, linkPathFresh) {
		t.Fatal("empty present .lerd.yaml via lerd link should run the wizard")
	}
	// Documents the regression: with fresh=false the present file skips the
	// wizard into a bare link, which is what the old runInit(false) call did.
	if initShouldRunWizard(true, false) {
		t.Fatal("guard sanity: a present file with fresh=false must skip the wizard")
	}
	// Absent config always runs the wizard regardless of fresh.
	if !initShouldRunWizard(false, false) {
		t.Fatal("absent .lerd.yaml should always run the wizard")
	}
}

// linkShouldRunWizard must treat an empty config the same as an absent one, so
// the link path routes into the (now forced) wizard.
func TestLinkShouldRunWizard_EmptyConfigRoutesToWizard(t *testing.T) {
	if !linkShouldRunWizard(false /* hasConfig */, true, false, false) {
		t.Fatal("no committed config + interactive + no arg + not worktree should run the wizard")
	}
	if linkShouldRunWizard(true /* hasConfig */, true, false, false) {
		t.Fatal("a real committed config should do a bare link, not the wizard")
	}
}
