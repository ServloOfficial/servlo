package authz

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// enrol turns TOTP on for an account and returns its secret and recovery codes.
func enrol(t *testing.T, store *AccountStore, name string) (string, []string) {
	t.Helper()
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}
	codes, err := store.EnableTOTP(name, secret)
	if err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	return secret, codes
}

// TOTP is optional, so an account without it signs in on the password alone.
func TestAccountTOTP_OffByDefault(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	account, ok := store.Lookup("alice")
	if !ok {
		t.Fatal("no account")
	}
	if account.TOTPEnabled {
		t.Error("a fresh account has TOTP on")
	}
}

// The secret is stored so codes can be checked, and it is exactly as sensitive
// as a password: anyone holding it can generate the second factor forever.
func TestAccountTOTP_SecretNeverLeavesTheStore(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	secret, _ := enrol(t, store, "alice")

	if account, _ := store.Lookup("alice"); account.TOTPSecret != "" {
		t.Error("Lookup returned the TOTP secret")
	}
	for _, account := range store.List() {
		if account.TOTPSecret != "" {
			t.Error("List returned the TOTP secret")
		}
		if len(account.RecoveryHashes) != 0 {
			t.Error("List returned the recovery hashes")
		}
	}
	// It is on disk, because verifying needs it. What must not be there is the
	// recovery codes in the form they were shown.
	data, err := os.ReadFile(AccountsPath())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !strings.Contains(string(data), secret) {
		t.Error("the secret was not stored, so no code can ever be checked")
	}
}

// With TOTP on, the password alone is not enough. That is the entire feature.
func TestAccountTOTP_PasswordAloneStopsBeingEnough(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	secret, _ := enrol(t, store, "alice")

	if _, ok := store.Authenticate("alice", "a long enough passphrase"); ok {
		t.Fatal("the password alone still signs in with TOTP enabled")
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", totpAt(secret, time.Now(), totpDigits)); !ok {
		t.Error("password plus a valid code was refused")
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", "000000"); ok {
		// A code that happens to be 000000 would make this flaky; the odds are
		// one in a million and the check below covers the real case.
		if totpAt(secret, time.Now(), totpDigits) != "000000" {
			t.Error("a wrong code was accepted")
		}
	}
	// And a right code with a wrong password is still nothing.
	if _, ok := store.AuthenticateWithCode("alice", "wrong", totpAt(secret, time.Now(), totpDigits)); ok {
		t.Error("a valid code signed in without the password")
	}
}

// A recovery code stands in for the second factor, once.
func TestAccountTOTP_RecoveryCodeWorksOnce(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, codes := enrol(t, store, "alice")

	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", codes[0]); !ok {
		t.Fatal("a recovery code was refused")
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", codes[0]); ok {
		t.Error("a recovery code worked twice")
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", codes[1]); !ok {
		t.Error("spending one recovery code invalidated another")
	}
}

// Turning it off is the CLI reset path: the operator with a shell can always
// get back in, whatever happened to the phone.
func TestAccountTOTP_DisableRestoresPasswordOnly(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	enrol(t, store, "alice")

	if err := store.DisableTOTP("alice"); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	if _, ok := store.Authenticate("alice", "a long enough passphrase"); !ok {
		t.Error("the password alone does not sign in after TOTP was turned off")
	}
	// The secret and the codes go with it. Leaving them would mean turning it
	// back on silently restored a factor the operator thought was gone.
	data, err := os.ReadFile(AccountsPath())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if strings.Contains(string(data), "totp_secret") {
		t.Error("the TOTP secret survived being disabled")
	}
}

// Fresh codes replace the old ones outright, so a printed sheet someone lost
// stops working the moment new ones are made.
func TestAccountTOTP_RegeneratingCodesInvalidatesTheOldOnes(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, old := enrol(t, store, "alice")

	fresh, err := store.RegenerateRecoveryCodes("alice")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes: %v", err)
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", old[0]); ok {
		t.Error("an old recovery code still works after regenerating")
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", fresh[0]); !ok {
		t.Error("a fresh recovery code does not work")
	}
}

// The login route asks for a code only when the account has TOTP on, and says
// so rather than answering "wrong password".
func TestLoginWithTOTP_AsksForTheCode(t *testing.T) {
	guard := testGuard(t)
	secret, codes := enrol(t, guard.Accounts, "alice")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"alice","password":"a long enough passphrase"}`))
	req.RemoteAddr = "203.0.113.9:54321"
	guard.HandleLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "code") {
		t.Errorf("the response does not ask for a code: %q", rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a login missing its second factor set a cookie")
	}

	// With the code, it works.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"alice","password":"a long enough passphrase","code":"`+totpAt(secret, time.Now(), totpDigits)+`"}`))
	req.RemoteAddr = "203.0.113.9:54321"
	guard.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with a code: status = %d (%s)", rec.Code, rec.Body.String())
	}

	// And so does a recovery code.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"alice","password":"a long enough passphrase","code":"`+codes[0]+`"}`))
	req.RemoteAddr = "203.0.113.9:54321"
	guard.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with a recovery code: status = %d (%s)", rec.Code, rec.Body.String())
	}
}

// A wrong code counts as a failed attempt. Without that, the second factor is
// six digits an attacker can try a million times.
func TestLoginWithTOTP_AWrongCodeCountsAgainstTheLimiter(t *testing.T) {
	guard := testGuard(t)
	enrol(t, guard.Accounts, "alice")

	var last int
	for i := 0; i < freeAttempts+3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"username":"alice","password":"a long enough passphrase","code":"000000"}`))
		req.RemoteAddr = "203.0.113.9:54321"
		guard.HandleLogin(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("status after repeated wrong codes = %d, want 429", last)
	}
}

// "That account needs a code" must only follow a correct password. Saying it
// after a wrong one tells an attacker the account exists and has TOTP on,
// which is exactly the enumeration the single failure message avoids.
func TestLoginWithTOTP_DoesNotAskForACodeUntilThePasswordIsRight(t *testing.T) {
	guard := testGuard(t)
	enrol(t, guard.Accounts, "alice")

	wrongPassword := loginBody(t, guard, `{"username":"alice","password":"wrong password here"}`)
	unknownAccount := loginBody(t, guard, `{"username":"mallory","password":"wrong password here"}`)
	if wrongPassword != unknownAccount {
		t.Errorf("a wrong password on a TOTP account answers differently from an unknown one:\n%q\n%q",
			wrongPassword, unknownAccount)
	}

	// And the right password does ask, or the operator is told nothing about
	// the second thing the form wants.
	rightPassword := loginBody(t, guard, `{"username":"alice","password":"a long enough passphrase"}`)
	if !strings.Contains(rightPassword, "code") {
		t.Errorf("a correct password on a TOTP account does not ask for a code: %q", rightPassword)
	}
}

func loginBody(t *testing.T, guard *Guard, body string) string {
	t.Helper()
	// A fresh limiter each time, so the lockout from the previous attempt does
	// not answer for the one under test.
	guard.Limiter = NewLimiter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:54321"
	guard.HandleLogin(rec, req)
	return rec.Body.String()
}

// The panel shows how many recovery codes are left, so an operator knows when
// to make more. The count has to survive the redaction the hashes do not.
func TestAccountTOTP_RecoveryCountSurvivesRedaction(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, codes := enrol(t, store, "alice")

	account, _ := store.Lookup("alice")
	if got := account.RecoveryCodesLeft(); got != len(codes) {
		t.Errorf("recovery codes left = %d, want %d", got, len(codes))
	}
	if _, ok := store.AuthenticateWithCode("alice", "a long enough passphrase", codes[0]); !ok {
		t.Fatal("a recovery code was refused")
	}
	account, _ = store.Lookup("alice")
	if got := account.RecoveryCodesLeft(); got != len(codes)-1 {
		t.Errorf("after spending one, recovery codes left = %d, want %d", got, len(codes)-1)
	}
}
