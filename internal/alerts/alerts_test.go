package alerts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return dir
}

func noMail(t *testing.T) *[]Alert {
	t.Helper()
	var sent []Alert
	prev := send
	t.Cleanup(func() { send = prev })
	send = func(a Alert) error {
		sent = append(sent, a)
		return nil
	}
	return &sent
}

// An alert is written down before it is emailed. Panel SMTP is optional and a
// mail server can be down, and an alert that only ever existed as an email
// nobody received is the failure the panel list exists to prevent.
func TestRaise_RecordsBeforeItSends(t *testing.T) {
	withHome(t)
	sent := noMail(t)

	if err := Raise(Alert{Kind: KindBackupFailed, Site: "acme", Message: "spaces unreachable"}); err != nil {
		t.Fatal(err)
	}
	open, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Site != "acme" {
		t.Fatalf("the alert was not recorded: %+v", open)
	}
	if len(*sent) != 1 {
		t.Error("the alert was not sent")
	}
}

// Mail failing must not lose the alert. It is in the panel either way, which is
// the whole reason it is written first.
func TestRaise_KeepsTheAlertWhenTheMailFails(t *testing.T) {
	withHome(t)
	prev := send
	t.Cleanup(func() { send = prev })
	send = func(Alert) error { return os.ErrPermission }

	if err := Raise(Alert{Kind: KindSiteDown, Site: "acme", Message: "502"}); err == nil {
		t.Error("a failed send reported success, so nobody knows the email did not go")
	}
	open, _ := List()
	if len(open) != 1 {
		t.Errorf("the alert was lost when the mail failed: %+v", open)
	}
}

// The same failure every five minutes is one alert, not two hundred. An inbox
// that gets flooded is an inbox that starts filtering servlo out.
func TestRaise_DoesNotRepeatTheSameFailure(t *testing.T) {
	withHome(t)
	sent := noMail(t)

	for i := 0; i < 5; i++ {
		if err := Raise(Alert{Kind: KindSiteDown, Site: "acme", Message: "502"}); err != nil {
			t.Fatal(err)
		}
	}
	open, _ := List()
	if len(open) != 1 {
		t.Errorf("the same failure was recorded %d times", len(open))
	}
	if len(*sent) != 1 {
		t.Errorf("the same failure was emailed %d times", len(*sent))
	}
}

// A different site's version of the same failure is a different alert. Folding
// them together would hide the second site being down.
func TestRaise_KeepsSitesApart(t *testing.T) {
	withHome(t)
	noMail(t)

	_ = Raise(Alert{Kind: KindSiteDown, Site: "acme", Message: "502"})
	_ = Raise(Alert{Kind: KindSiteDown, Site: "shopfront", Message: "502"})

	open, _ := List()
	if len(open) != 2 {
		t.Errorf("two sites down produced %d alerts", len(open))
	}
}

// Clearing is what makes the list mean something. An alert that stays after the
// thing it is about recovered teaches an operator to ignore the list.
func TestClear_TakesTheAlertAwayWhenItRecovers(t *testing.T) {
	withHome(t)
	noMail(t)

	_ = Raise(Alert{Kind: KindSiteDown, Site: "acme", Message: "502"})
	if err := Clear(KindSiteDown, "acme"); err != nil {
		t.Fatal(err)
	}
	open, _ := List()
	if len(open) != 0 {
		t.Errorf("the alert survived the recovery: %+v", open)
	}

	// And it can be raised again afterwards, or a site that flaps is only ever
	// reported once.
	sent := noMail(t)
	_ = Raise(Alert{Kind: KindSiteDown, Site: "acme", Message: "502"})
	if len(*sent) != 1 {
		t.Error("a site that recovered and failed again was not reported the second time")
	}
}

// The store holds what is wrong with a production server, so it is not readable
// by other accounts on it.
func TestRaise_WritesTheStorePrivate(t *testing.T) {
	dir := withHome(t)
	noMail(t)
	_ = Raise(Alert{Kind: KindDiskFilling, Message: "92% full"})

	info, err := os.Stat(filepath.Join(dir, "servlo", storeFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("the alert store is %04o, want 0600", perm)
	}
}

// An alert says when, so a list read a week later is a history rather than a
// pile.
func TestRaise_StampsWhenItHappened(t *testing.T) {
	withHome(t)
	noMail(t)
	before := time.Now().Add(-time.Second)

	_ = Raise(Alert{Kind: KindDeployFailed, Site: "acme", Message: "composer failed"})
	open, _ := List()
	if len(open) != 1 {
		t.Fatal("no alert")
	}
	if open[0].At.Before(before) {
		t.Errorf("the alert is stamped %v, which is not when it happened", open[0].At)
	}
	if !strings.Contains(open[0].Message, "composer") {
		t.Errorf("the message did not survive: %q", open[0].Message)
	}
}
