package authz

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func testAccounts(t *testing.T) *AccountStore {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := OpenAccounts()
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	return store
}

// Until someone sets a password there is no account, and the panel has to know
// that so it can walk the operator through creating one rather than presenting
// a login form nothing can satisfy.
func TestAccounts_EmptyUntilOneIsCreated(t *testing.T) {
	store := testAccounts(t)
	if store.Any() {
		t.Fatal("a fresh install already has an account")
	}
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !store.Any() {
		t.Error("an account was created and the store still reports none")
	}
}

func TestAccounts_AuthenticateAcceptsOnlyTheRightPassword(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	account, ok := store.Authenticate("alice", "a long enough passphrase")
	if !ok {
		t.Fatal("the right password was rejected")
	}
	if account.Role != RoleAdmin {
		t.Errorf("role = %q, want %q", account.Role, RoleAdmin)
	}
	for _, attempt := range [][2]string{
		{"alice", "wrong"},
		{"alice", ""},
		{"bob", "a long enough passphrase"},
		{"", "a long enough passphrase"},
		{"ALICE", "a long enough passphrase"},
	} {
		if _, ok := store.Authenticate(attempt[0], attempt[1]); ok {
			t.Errorf("authenticated %q / %q", attempt[0], attempt[1])
		}
	}
}

// The file holds hashes. Anyone who can read it should learn who has an
// account, not how to sign in as them.
func TestAccounts_StoreHoldsNoPlaintext(t *testing.T) {
	store := testAccounts(t)
	const password = "a long enough passphrase"
	if _, err := store.Create("alice", password, RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(AccountsPath())
	if err != nil {
		t.Fatalf("reading the account store: %v", err)
	}
	if strings.Contains(string(data), password) {
		t.Fatal("the account store contains the password in the clear")
	}
	if !strings.Contains(string(data), "$argon2id$") {
		t.Error("the stored hash is not argon2id")
	}
	info, _ := os.Stat(AccountsPath())
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("account store mode = %04o, want 0600", mode)
	}
}

// A short password on a panel facing the internet is the whole attack. The
// floor is a length rather than a character-class rule, because forcing a
// symbol produces Password1! and forcing length produces a passphrase.
func TestAccounts_RefusesAPasswordTooShortToMatter(t *testing.T) {
	store := testAccounts(t)
	for _, password := range []string{"", "short", "1234567890"} {
		if _, err := store.Create("alice", password, RoleAdmin); err == nil {
			t.Errorf("Create accepted the password %q", password)
		}
	}
	if store.Any() {
		t.Error("a refused Create still made an account")
	}
}

func TestAccounts_RefusesADuplicateName(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create("alice", "another long passphrase", RoleAdmin); err == nil {
		t.Error("a second account was created with the same name")
	}
}

// Changing a password must take effect immediately, and the old one must stop
// working, which is the whole point of changing it.
func TestAccounts_SetPassword(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetPassword("alice", "a different long passphrase"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, ok := store.Authenticate("alice", "a long enough passphrase"); ok {
		t.Error("the old password still works")
	}
	if _, ok := store.Authenticate("alice", "a different long passphrase"); !ok {
		t.Error("the new password does not work")
	}
}

// An install upgrading from the inherited config carries a bcrypt hash. It has
// to keep working, and signing in with it should quietly leave an Argon2id one
// behind so the bcrypt hash is used exactly once more.
func TestAccounts_UpgradesAnInheritedHashOnSignIn(t *testing.T) {
	store := testAccounts(t)
	// bcrypt cost 10 for "secret".
	const inherited = "$2a$10$PyYSdhGCPm5wN5NkhjEUReOq2pBb9xIUb8SpgGn1SeHta.xyb1A76"
	if err := store.Adopt("alice", inherited, RoleAdmin); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if _, ok := store.Authenticate("alice", "secret"); !ok {
		t.Fatal("an inherited bcrypt hash no longer signs in")
	}
	data, err := os.ReadFile(AccountsPath())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if strings.Contains(string(data), inherited) {
		t.Error("signing in did not replace the inherited bcrypt hash")
	}
	if _, ok := store.Authenticate("alice", "secret"); !ok {
		t.Error("the password stopped working after the hash was upgraded")
	}
}

// Both the panel and the CLI hold this, so a password set in a shell has to be
// the password the panel checks against on the next request.
func TestAccounts_IsVisibleToAnotherProcess(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	other, err := OpenAccounts()
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	if err := other.SetPassword("alice", "a different long passphrase"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, ok := store.Authenticate("alice", "a different long passphrase"); !ok {
		t.Error("a password set in another process is not what the panel checks")
	}
}

func TestAccounts_ListAndDelete(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create("bob", "a long enough passphrase", RoleDeveloper); err != nil {
		t.Fatalf("Create: %v", err)
	}

	accounts := store.List()
	if len(accounts) != 2 {
		t.Fatalf("List returned %d accounts, want 2", len(accounts))
	}
	for _, account := range accounts {
		if account.PasswordHash != "" {
			t.Error("List exposed a password hash")
		}
	}

	if err := store.Delete("bob"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.Authenticate("bob", "a long enough passphrase"); ok {
		t.Error("a deleted account still authenticates")
	}
}

// Deleting the last admin leaves a panel nobody can administer, which is an
// outage that needs a shell on the box to undo.
func TestAccounts_RefusesToDeleteTheLastAdmin(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create("bob", "a long enough passphrase", RoleDeveloper); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Delete("alice"); err == nil {
		t.Error("the last admin was deleted")
	}
	if _, ok := store.Authenticate("alice", "a long enough passphrase"); !ok {
		t.Error("the refused delete removed the account anyway")
	}
}

// The form answers a wrong password and an unknown account with the same words,
// on purpose: telling them apart turns it into a way to enumerate account
// names. It has to answer them in the same time too. An unknown name that never
// reaches the password hash comes back in microseconds where a real one takes
// tens of milliseconds, and a 2000x gap is not a side channel anybody needs
// statistics to read.
func TestAuthenticate_AnUnknownAccountCostsWhatAKnownOneCosts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	store, err := OpenAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatal(err)
	}

	// The median of a handful, because a busy machine can stall any single
	// attempt and the claim is about the work done, not about one reading.
	median := func(name string) time.Duration {
		const runs = 5
		var taken []time.Duration
		for i := 0; i < runs; i++ {
			start := time.Now()
			store.AuthenticateWithOutcome(name, "not the passphrase", "")
			taken = append(taken, time.Since(start))
		}
		sort.Slice(taken, func(i, j int) bool { return taken[i] < taken[j] })
		return taken[runs/2]
	}

	known, unknown := median("alice"), median("nobody")
	// A quarter, not parity: the point is that the hash ran, and a bound that
	// tight would fail on a loaded runner for no reason.
	if unknown*4 < known {
		t.Errorf("an unknown account was refused in %v where a known one took %v (%.0fx faster), "+
			"so the login form says which names exist even though it will not print it",
			unknown, known, float64(known)/float64(unknown))
	}
}
