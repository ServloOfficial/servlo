package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestResolveSitePath_emptyBranchReturnsSitePath: when branch is unset,
// the resolver hands back site.Path so the existing code path is preserved.
func TestResolveSitePath_emptyBranchReturnsSitePath(t *testing.T) {
	tmp := t.TempDir()
	s := &config.Site{Path: tmp, Domains: []string{"acme.test"}}
	got := resolveSitePath(s, "")
	if got != tmp {
		t.Errorf("resolveSitePath(\"\") = %q, want %q", got, tmp)
	}
}

// TestResolveSitePath_unknownBranchReturnsEmpty: an unknown branch returns ""
// so callers can 404 instead of silently falling back to the parent site.
func TestResolveSitePath_unknownBranchReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	s := &config.Site{Path: tmp, Domains: []string{"acme.test"}}
	got := resolveSitePath(s, "feat-x")
	if got != "" {
		t.Errorf("resolveSitePath(\"feat-x\") = %q, want \"\"", got)
	}
}

// TestResolveSitePath_knownWorktreeReturnsWorktreePath: a known sanitized
// branch maps to the worktree checkout path.
func TestResolveSitePath_knownWorktreeReturnsWorktreePath(t *testing.T) {
	tmp, checkoutPath := setupWorktreeFixture(t, "feat-a")
	s := &config.Site{Path: tmp, Domains: []string{"acme.test"}}
	got := resolveSitePath(s, "feat-a")
	if got != checkoutPath {
		t.Errorf("resolveSitePath(\"feat-a\") = %q, want %q", got, checkoutPath)
	}
}

// setupWorktreeFixture creates a fake git main-repo at a fresh temp dir with one
// active worktree on the given branch, and returns the main-repo path and the
// worktree checkout path.
func setupWorktreeFixture(t *testing.T, branch string) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	wtDir := filepath.Join(gitDir, "worktrees", branch)
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatal(err)
	}
	checkoutPath := filepath.Join(t.TempDir(), branch+"-checkout")
	if err := os.Mkdir(checkoutPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(checkoutPath, ".git")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return tmp, checkoutPath
}
