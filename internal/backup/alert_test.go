package backup

import (
	"errors"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/alerts"
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

// A backup runs on a timer in the middle of the night with nobody watching, so
// a failure that only reached the journal is a server that quietly has no
// backups.
func TestReport_RaisesWhenTheBackupFailed(t *testing.T) {
	raised, _ := captureAlerts(t)

	Report("acme", Record{}, errors.New("mysqldump: access denied"))

	if len(*raised) != 1 || (*raised)[0].Kind != alerts.KindBackupFailed {
		t.Fatalf("raised %+v, want one backup-failed alert", *raised)
	}
	if (*raised)[0].Site != "acme" {
		t.Errorf("the alert is against %q", (*raised)[0].Site)
	}
	if !strings.Contains((*raised)[0].Message, "access denied") {
		t.Errorf("the alert does not carry the reason:\n%s", (*raised)[0].Message)
	}
}

// A destination that could not be reached counts. The command reports it as a
// warning because the archive is on this server and usable, but an archive that
// only exists on the machine it is a backup of is not a backup, and the day
// anybody finds out is the day the machine is gone.
func TestReport_RaisesWhenNothingLeftTheServer(t *testing.T) {
	raised, _ := captureAlerts(t)

	Report("acme", Record{SendErrors: []error{errors.New("spaces: 403 forbidden")}}, nil)

	if len(*raised) != 1 || (*raised)[0].Kind != alerts.KindBackupFailed {
		t.Fatalf("raised %+v, want an alert about the copy that did not happen", *raised)
	}
	if !strings.Contains((*raised)[0].Message, "offsite") {
		t.Errorf("the alert does not explain what is missing:\n%s", (*raised)[0].Message)
	}
	if !strings.Contains((*raised)[0].Message, "403") {
		t.Errorf("the alert does not carry the reason:\n%s", (*raised)[0].Message)
	}
}

// Every destination that failed is named. Two exist so that one being
// unreachable is survivable, and an alert that mentioned only the first would
// have an operator fix one and think they were done.
func TestReport_NamesEveryDestinationThatFailed(t *testing.T) {
	raised, _ := captureAlerts(t)

	Report("acme", Record{SendErrors: []error{
		errors.New("spaces: 403 forbidden"),
		errors.New("offsite: connection refused"),
	}}, nil)

	for _, want := range []string{"403", "connection refused"} {
		if !strings.Contains((*raised)[0].Message, want) {
			t.Errorf("the alert leaves out %q:\n%s", want, (*raised)[0].Message)
		}
	}
}

// A backup that worked takes the alert away, so an operator who fixed the
// credentials sees that it worked.
func TestReport_ClearsWhenTheBackupWorked(t *testing.T) {
	raised, cleared := captureAlerts(t)

	Report("acme", Record{}, nil)

	if len(*raised) != 0 {
		t.Errorf("a backup that worked raised %+v", *raised)
	}
	if len(*cleared) != 1 || (*cleared)[0] != alerts.KindBackupFailed+"/acme" {
		t.Errorf("cleared %v, want the backup alert cleared", *cleared)
	}
}

// A backup that cannot be restored is not a backup, and this is the only check
// that ever asks. It has its own alert because the fix is different: a failed
// backup means nothing was written, an unverifiable one means something was.
func TestReportVerify_RaisesItsOwnKind(t *testing.T) {
	raised, _ := captureAlerts(t)

	ReportVerify("acme", "acme-20260809-030000", errors.New("the dump would not load"))

	if len(*raised) != 1 || (*raised)[0].Kind != alerts.KindBackupUnverified {
		t.Fatalf("raised %+v, want a backup-unverified alert", *raised)
	}
	if !strings.Contains((*raised)[0].Message, "acme-20260809-030000") {
		t.Errorf("the alert does not say which archive:\n%s", (*raised)[0].Message)
	}
}

func TestReportVerify_ClearsWhenTheArchiveRestores(t *testing.T) {
	raised, cleared := captureAlerts(t)

	ReportVerify("acme", "acme-20260809-030000", nil)

	if len(*raised) != 0 {
		t.Errorf("a backup that restored raised %+v", *raised)
	}
	if len(*cleared) != 1 || (*cleared)[0] != alerts.KindBackupUnverified+"/acme" {
		t.Errorf("cleared %v, want the verify alert cleared", *cleared)
	}
}
