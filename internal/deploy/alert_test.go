package deploy

import (
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/alerts"
	"github.com/realrashid/servlo/internal/config"
)

func captureAlerts(t *testing.T) (*[]alerts.Alert, *[]string) {
	t.Helper()
	var raised []alerts.Alert
	var cleared []string
	prevRaise, prevClear := raise, clearAlert
	t.Cleanup(func() { raise, clearAlert = prevRaise, prevClear })
	raise = func(a alerts.Alert) error { raised = append(raised, a); return nil }
	clearAlert = func(kind, site string) error { cleared = append(cleared, kind+"/"+site); return nil }
	return &raised, &cleared
}

// The deploys that need an alert are the ones nobody watched: a webhook firing
// on a push, or a browser tab closed before it finished.
func TestRecord_RaisesOnAFailedDeploy(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	raised, _ := captureAlerts(t)

	err := Record(&config.Site{Name: "acme"}, Entry{
		OK: false, Error: "composer install exited 1", Subject: "Bump the invoice totals",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(*raised) != 1 || (*raised)[0].Kind != alerts.KindDeployFailed {
		t.Fatalf("raised %+v, want one deploy-failed alert", *raised)
	}
	if !strings.Contains((*raised)[0].Message, "composer install exited 1") {
		t.Errorf("the alert does not carry the reason:\n%s", (*raised)[0].Message)
	}
	// The commit subject is in it because a SHA alone does not tell an operator
	// reading an email at midnight which change broke.
	if !strings.Contains((*raised)[0].Message, "Bump the invoice totals") {
		t.Errorf("the alert does not say which change failed:\n%s", (*raised)[0].Message)
	}
}

// The next deploy that works takes the alert away, which is the only way an
// operator learns the fix landed without opening the panel.
func TestRecord_ClearsOnASuccessfulDeploy(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	raised, cleared := captureAlerts(t)

	if err := Record(&config.Site{Name: "acme"}, Entry{OK: true}); err != nil {
		t.Fatal(err)
	}

	if len(*raised) != 0 {
		t.Errorf("a deploy that worked raised %+v", *raised)
	}
	if len(*cleared) != 1 || (*cleared)[0] != alerts.KindDeployFailed+"/acme" {
		t.Errorf("cleared %v, want the deploy alert cleared", *cleared)
	}
}

// A deploy that failed without saying why still says something, because an
// empty alert body reads like a bug in servlo rather than a broken deploy.
func TestRecord_SaysSomethingWhenTheFailureHasNoReason(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	raised, _ := captureAlerts(t)

	if err := Record(&config.Site{Name: "acme"}, Entry{OK: false}); err != nil {
		t.Fatal(err)
	}
	if len(*raised) != 1 || strings.TrimSpace((*raised)[0].Message) == "" {
		t.Fatalf("raised %+v, want an alert that says something", *raised)
	}
}

// A deploy that could not be recorded raises nothing. An alert claiming a
// deploy the history has no entry for is an alert nobody can look into.
func TestRecord_RaisesNothingWhenTheHistoryCouldNotBeWritten(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	raised, cleared := captureAlerts(t)

	if err := Record(&config.Site{Name: "../escape"}, Entry{OK: false, Error: "boom"}); err == nil {
		t.Fatal("a site name that is not usable was recorded")
	}
	if len(*raised) != 0 || len(*cleared) != 0 {
		t.Errorf("raised %+v and cleared %v for a deploy that was never recorded", *raised, *cleared)
	}
}
