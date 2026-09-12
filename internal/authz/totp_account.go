package authz

import (
	"crypto/subtle"
	"fmt"
)

// Turning TOTP on, off, and checking it at sign-in.
//
// Optional, per the PRD. A panel behind a long passphrase and a doubling
// lockout is already hard to guess; this is for the operator who wants a
// stolen password not to be enough on its own.

// EnableTOTP turns the second factor on for an account and returns the recovery
// codes to show once. Enabling replaces any previous secret and codes, so
// re-enrolling a new phone does not leave the old one working.
//
// enrolledCounter is the step of the code that proved the enrolment, recorded as
// spent so the same code cannot also be the one that signs in.
func (s *AccountStore) EnableTOTP(name, secret string, enrolledCounter int64) ([]string, error) {
	if secret == "" {
		return nil, fmt.Errorf("no secret to enrol")
	}
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].Name != name {
			continue
		}
		accounts[i].TOTPSecret = secret
		accounts[i].TOTPEnabled = true
		accounts[i].RecoveryHashes = hashes
		// The code that proved the enrolment counts as spent, so it cannot also
		// be the code that signs in a minute later.
		accounts[i].TOTPLastCounter = enrolledCounter
		if err := s.save(accounts); err != nil {
			return nil, err
		}
		return codes, nil
	}
	return nil, fmt.Errorf("no account named %q", name)
}

// DisableTOTP turns it off, taking the secret and the unspent codes with it.
//
// This is the reset path: an operator with a shell can always get back in,
// whatever happened to the phone. Leaving the secret behind would mean turning
// it back on silently restored a factor they thought was gone.
func (s *AccountStore) DisableTOTP(name string) error {
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
		accounts[i].TOTPSecret = ""
		accounts[i].TOTPEnabled = false
		accounts[i].RecoveryHashes = nil
		return s.save(accounts)
	}
	return fmt.Errorf("no account named %q", name)
}

// RegenerateRecoveryCodes issues a fresh batch and discards the old, so a
// printed sheet someone mislaid stops working the moment new ones are made.
func (s *AccountStore) RegenerateRecoveryCodes(name string) ([]string, error) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].Name != name {
			continue
		}
		if accounts[i].TOTPSecret == "" {
			return nil, fmt.Errorf("%q has no second factor, so there is nothing to recover from", name)
		}
		accounts[i].RecoveryHashes = hashes
		if err := s.save(accounts); err != nil {
			return nil, err
		}
		return codes, nil
	}
	return nil, fmt.Errorf("no account named %q", name)
}

// Outcome is why a sign-in was refused, which the login route needs and the
// person at the form mostly does not.
//
// The distinction that matters: a wrong password and an unknown account answer
// identically, or the form becomes a way to enumerate account names. That a
// correct password still owes a second factor is different — the operator has
// to be told, or the form asks for two things and reports on one — and it says
// nothing to someone who does not already hold the password.
type Outcome int

const (
	// AuthFailed covers a wrong password and an unknown account alike.
	AuthFailed Outcome = iota
	AuthOK
	// AuthCodeRequired is only ever returned when the password was right.
	AuthCodeRequired
)

// AuthenticateWithCode is Authenticate plus the second factor.
//
// code is either a TOTP code or a recovery code; the caller does not have to
// know which, because the operator typing it into one box does not either. An
// account without TOTP ignores it entirely, so a client that always sends the
// field costs nothing.
func (s *AccountStore) AuthenticateWithCode(name, password, code string) (Account, bool) {
	account, outcome := s.authenticate(name, password, code)
	return account, outcome == AuthOK
}

// AuthenticateWithOutcome is AuthenticateWithCode with the reason attached.
func (s *AccountStore) AuthenticateWithOutcome(name, password, code string) (Account, Outcome) {
	return s.authenticate(name, password, code)
}

func (s *AccountStore) authenticate(name, password, code string) (Account, Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load()
	if err != nil {
		return Account{}, AuthFailed
	}

	i := -1
	for j := range accounts {
		if subtle.ConstantTimeCompare([]byte(accounts[j].Name), []byte(name)) == 1 {
			i = j
			break
		}
	}
	if i < 0 {
		// An unknown name is made to cost what a known one costs. The form
		// answers a wrong password and a name that does not exist with the same
		// words on purpose; without this it answers them in wildly different
		// times, and the clock says what the wording will not.
		VerifyPassword(decoyHash(), password)
		return Account{}, AuthFailed
	}

	// Once, for every outcome below. Verifying twice would cost a second-factor
	// sign-in double, and would say which accounts have a second factor in the
	// same currency the decoy above exists to stop spending.
	if !VerifyPassword(accounts[i].PasswordHash, password) {
		return Account{}, AuthFailed
	}

	// The password is known good from here, so anything said below tells the
	// holder of that password something they can act on and tells anyone else
	// nothing.
	changed := false
	if accounts[i].TOTPEnabled || accounts[i].TOTPSecret != "" {
		if code == "" {
			return Account{}, AuthCodeRequired
		}
		if counter, ok := VerifyTOTPCounter(accounts[i].TOTPSecret, code); ok {
			// Once. A code is valid for its own step and one either side, so
			// without this the code somebody watched being typed is good for
			// another minute and a half against a password they already have.
			if counter <= accounts[i].TOTPLastCounter {
				return Account{}, AuthFailed
			}
			accounts[i].TOTPLastCounter = counter
			changed = true
		} else {
			remaining, spent := ConsumeRecoveryCode(accounts[i].RecoveryHashes, code)
			if !spent {
				return Account{}, AuthFailed
			}
			accounts[i].RecoveryHashes = remaining
			changed = true
		}
	}

	// While the plaintext is in hand, so an inherited bcrypt hash is used
	// exactly once more.
	if NeedsRehash(accounts[i].PasswordHash) {
		if hash, err := HashPassword(password); err == nil {
			accounts[i].PasswordHash = hash
			changed = true
		}
	}
	if changed {
		// A failed write leaves a spent code spendable and an old hash in
		// place, which is the safe direction in both cases: the operator gets
		// in, and the window closes on its own thirty seconds later.
		_ = s.save(accounts)
	}
	return redact(accounts[i]), AuthOK
}
