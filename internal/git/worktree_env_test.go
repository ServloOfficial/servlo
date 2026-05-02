package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EnsureWorktreeEnv must materialise .env in a fresh worktree (git worktree
// add does not carry it across because the file is gitignored). The main
// repo's .env is the source; APP_URL is rewritten to the worktree domain.
func TestEnsureWorktreeEnv_copiesFromMainAndRewritesAppURL(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_NAME=acme\nAPP_URL=http://acme.test\nDB_HOST=mysql\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", false)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatalf("worktree .env not created: %v", err)
	}
	if !strings.Contains(string(got), "APP_URL=http://feat-a.acme.test") {
		t.Errorf("APP_URL not rewritten:\n%s", got)
	}
	if !strings.Contains(string(got), "DB_HOST=mysql") {
		t.Errorf(".env not copied in full:\n%s", got)
	}
}

// When the worktree already has its own .env, we keep it but realign APP_URL.
func TestEnsureWorktreeEnv_preservesExistingEnvAndRealignsURL(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	if err := os.WriteFile(filepath.Join(main, ".env"), []byte("APP_URL=http://main.test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	custom := "APP_URL=http://stale.test\nMY_KEY=keep-me\n"
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", true)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "APP_URL=https://feat-a.acme.test") {
		t.Errorf("APP_URL not realigned to https worktree:\n%s", got)
	}
	if !strings.Contains(string(got), "MY_KEY=keep-me") {
		t.Errorf("worktree-specific keys lost:\n%s", got)
	}
}

// No-op when the main repo has no .env (lerd should not invent one out of
// thin air; it simply has nothing to copy).
func TestEnsureWorktreeEnv_noopWhenMainHasNoEnv(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", false)

	if _, err := os.Stat(filepath.Join(wt, ".env")); !os.IsNotExist(err) {
		t.Errorf("expected no .env in worktree, got err=%v", err)
	}
}
