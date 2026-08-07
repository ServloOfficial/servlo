package authz

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// Accounts.
//
// Two roles, per PRD §7: an Admin who can do everything, and a Developer who
// reaches only the sites assigned to them. S5.4 gives Developer its assignment
// list and the enforcement; the field is here now so an account created today
// does not need migrating then.
//
// The store is a file the panel and the CLI both hold, read through on every
// operation. A password set in a shell has to be the password the panel checks
// on the next request, not at the next restart.

// Role is what an account may do.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
)

// Valid reports whether r is a role servlo knows. An unknown role is not a
// role with no permissions, it is a corrupt record, and callers treat it as a
// refusal rather than a silent downgrade.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleDeveloper }

// minPasswordLength is a floor rather than a character-class rule. Requiring a
// symbol produces Password1!; requiring length produces a passphrase, and
// length is what actually costs an attacker.
const minPasswordLength = 12

// Account is one panel user.
type Account struct {
	Name         string    `json:"name"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Role         Role      `json:"role"`
	Created      time.Time `json:"created"`
	// Sites are the domains a Developer may act on. Empty for an Admin, who
	// reaches everything. S5.4 enforces it.
	Sites []string `json:"sites,omitempty"`
}

// AccountStore is the on-disk set of accounts.
type AccountStore struct {
	mu sync.Mutex
}

// AccountsPath is where the accounts live: the config directory, beside the
// other things an operator would back up, rather than the data directory that
// holds derived state.
func AccountsPath() string { return filepath.Join(config.ConfigDir(), "accounts.json") }

// OpenAccounts returns a handle to the account store.
func OpenAccounts() (*AccountStore, error) {
	if err := os.MkdirAll(filepath.Dir(AccountsPath()), 0700); err != nil {
		return nil, fmt.Errorf("creating the account directory: %w", err)
	}
	return &AccountStore{}, nil
}

// Any reports whether any account exists. A fresh install has none, and the
// panel needs to know so it can walk the operator through creating the first
// rather than showing a login form nothing can satisfy.
func (s *AccountStore) Any() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.load()
	return err == nil && len(accounts) > 0
}

// Create adds an account.
func (s *AccountStore) Create(name, password string, role Role) (Account, error) {
	if err := validateAccountName(name); err != nil {
		return Account{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return Account{}, err
	}
	if !role.Valid() {
		return Account{}, fmt.Errorf("%q is not a role", role)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Account{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return Account{}, err
	}
	for _, account := range accounts {
		if account.Name == name {
			return Account{}, fmt.Errorf("an account named %q already exists", name)
		}
	}
	created := Account{Name: name, PasswordHash: hash, Role: role, Created: time.Now()}
	if err := s.save(append(accounts, created)); err != nil {
		return Account{}, err
	}
	created.PasswordHash = ""
	return created, nil
}

// Adopt adds an account with a hash servlo did not make, which is how the
// credentials an install carried before this landed become an account.
func (s *AccountStore) Adopt(name, passwordHash string, role Role) error {
	if err := validateAccountName(name); err != nil {
		return err
	}
	if passwordHash == "" {
		return fmt.Errorf("no password hash to adopt")
	}
	if !role.Valid() {
		return fmt.Errorf("%q is not a role", role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if account.Name == name {
			return fmt.Errorf("an account named %q already exists", name)
		}
	}
	return s.save(append(accounts, Account{
		Name: name, PasswordHash: passwordHash, Role: role, Created: time.Now(),
	}))
}

// Authenticate checks a name and password, and quietly replaces a hash that is
// due for rehashing while it holds the plaintext. An inherited bcrypt hash is
// therefore used exactly once more.
func (s *AccountStore) Authenticate(name, password string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return Account{}, false
	}
	for i := range accounts {
		if subtle.ConstantTimeCompare([]byte(accounts[i].Name), []byte(name)) != 1 {
			continue
		}
		if !VerifyPassword(accounts[i].PasswordHash, password) {
			return Account{}, false
		}
		if NeedsRehash(accounts[i].PasswordHash) {
			if hash, err := HashPassword(password); err == nil {
				accounts[i].PasswordHash = hash
				// A failed write leaves the old hash, which still verifies, so
				// the sign-in stands and the upgrade retries next time.
				_ = s.save(accounts)
			}
		}
		found := accounts[i]
		found.PasswordHash = ""
		return found, true
	}
	return Account{}, false
}

// Lookup returns an account by name, without its hash.
func (s *AccountStore) Lookup(name string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return Account{}, false
	}
	for _, account := range accounts {
		if account.Name == name {
			account.PasswordHash = ""
			return account, true
		}
	}
	return Account{}, false
}

// SetPassword replaces an account's password.
func (s *AccountStore) SetPassword(name, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].Name == name {
			accounts[i].PasswordHash = hash
			return s.save(accounts)
		}
	}
	return fmt.Errorf("no account named %q", name)
}

// SetRole changes an account's role, refusing to remove the last admin.
func (s *AccountStore) SetRole(name string, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("%q is not a role", role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].Name != name {
			continue
		}
		if accounts[i].Role == RoleAdmin && role != RoleAdmin && countAdmins(accounts) == 1 {
			return fmt.Errorf("%q is the only admin, so demoting it would leave the panel with none", name)
		}
		accounts[i].Role = role
		return s.save(accounts)
	}
	return fmt.Errorf("no account named %q", name)
}

// List returns the accounts, by name, without their hashes.
func (s *AccountStore) List() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return nil
	}
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		account.PasswordHash = ""
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Delete removes an account, refusing to remove the last admin: that leaves a
// panel nobody can administer, an outage needing a shell on the box to undo.
func (s *AccountStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return err
	}
	kept := make([]Account, 0, len(accounts))
	var removed *Account
	for i := range accounts {
		if accounts[i].Name == name {
			removed = &accounts[i]
			continue
		}
		kept = append(kept, accounts[i])
	}
	if removed == nil {
		return fmt.Errorf("no account named %q", name)
	}
	if removed.Role == RoleAdmin && countAdmins(accounts) == 1 {
		return fmt.Errorf("%q is the only admin, so removing it would leave the panel with none", name)
	}
	return s.save(kept)
}

// ValidatePassword reports whether a password clears the floor.
func ValidatePassword(password string) error {
	if len(password) < minPasswordLength {
		return fmt.Errorf("a password needs at least %d characters; a passphrase of a few words is easier to remember and harder to guess", minPasswordLength)
	}
	return nil
}

func validateAccountName(name string) error {
	if name == "" {
		return fmt.Errorf("an account needs a name")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("%q is not a usable account name: lowercase letters, digits, dot, dash and underscore only", name)
		}
	}
	return nil
}

func countAdmins(accounts []Account) int {
	n := 0
	for _, account := range accounts {
		if account.Role == RoleAdmin {
			n++
		}
	}
	return n
}

func (s *AccountStore) load() ([]Account, error) {
	data, err := os.ReadFile(AccountsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the account store: %w", err)
	}
	var accounts []Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		// Unlike the session store, a corrupt account file is not treated as
		// empty: that would present the first-run setup form on a machine that
		// has accounts, and let anyone reaching it make themselves an admin.
		return nil, fmt.Errorf("the account store is unreadable: %w", err)
	}
	return accounts, nil
}

func (s *AccountStore) save(accounts []Account) error {
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(AccountsPath())
	tmp, err := os.CreateTemp(dir, ".accounts-*.json")
	if err != nil {
		return fmt.Errorf("creating a temporary account store: %w", err)
	}
	defer os.Remove(tmp.Name())
	// Created 0600 rather than chmodded after, so it is never briefly readable.
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), AccountsPath())
}
