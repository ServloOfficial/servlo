package dbdump

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbexec"
)

// scriptedRuns answers each call in turn, so a test can make the count come
// back with a number and the create and drop come back empty.
func scriptedRuns(t *testing.T, replies []string, fail map[int]error) *[][]string {
	t.Helper()
	var runs [][]string
	prev, prevIn := dbexec.Run, dbexec.RunWithInput
	t.Cleanup(func() { dbexec.Run, dbexec.RunWithInput = prev, prevIn })

	answer := func(args []string, stdout io.Writer) ([]byte, error) {
		i := len(runs)
		runs = append(runs, args)
		if err, bad := fail[i]; bad {
			// Clients echo their arguments on failure, which is exactly how a
			// password ends up in an error. The fake does the same.
			return []byte("mysql: access denied using password adminpassword"), err
		}
		if i < len(replies) {
			if stdout != nil {
				_, _ = io.WriteString(stdout, replies[i])
				return nil, nil
			}
			return []byte(replies[i]), nil
		}
		return nil, nil
	}
	dbexec.Run = func(_ context.Context, args []string, _ []string, stdout io.Writer) ([]byte, error) {
		return answer(args, stdout)
	}
	dbexec.RunWithInput = func(_ context.Context, args []string, _ []string, _ io.Reader) ([]byte, error) {
		return answer(args, nil)
	}
	return &runs
}

func verifySpec() *config.EntitySpec {
	return &config.EntitySpec{
		Kind:  "databases",
		Image: "docker.io/library/mysql:8.4",
		Actions: map[string]config.EntityAction{
			"remote_create":       {Exec: "mysql -h {{host}} -e 'CREATE DATABASE `{{database}}`'"},
			"remote_import":       {Exec: "mysql -h {{host}} {{database}}"},
			"remote_drop":         {Exec: "mysql -h {{host}} -e 'DROP DATABASE `{{database}}`'"},
			"remote_count_tables": {Exec: "mysql -h {{host}} -sN -e 'SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = \"{{database}}\"'"},
		},
	}
}

func managedConn() dbconn.Connection {
	return dbconn.Connection{Name: "managed", Family: "mysql", Host: "db.example.net", Port: 25060, User: "doadmin", Password: "adminpassword"}
}

// The point of a test restore: the dump goes into a database of its own, the
// tables are counted, and the scratch database is taken away again. A backup
// that has never been restored is not a backup, and a green tick that checked
// nothing is worse than no check at all.
func TestVerify_LoadsCountsAndTearsDown(t *testing.T) {
	runs := scriptedRuns(t, []string{"", "", "42"}, nil)

	res, err := verifyWith(verifySpec(), managedConn(), "acme_verify_1", strings.NewReader("-- dump"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Tables != 42 {
		t.Errorf("counted %d tables, want 42", res.Tables)
	}
	joined := make([]string, len(*runs))
	for i, r := range *runs {
		joined[i] = strings.Join(r, " ")
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{"CREATE DATABASE", "DROP DATABASE"} {
		if !strings.Contains(all, want) {
			t.Errorf("the verify did not %s:\n%s", want, all)
		}
	}
	if !strings.Contains(joined[len(joined)-1], "DROP DATABASE") {
		t.Errorf("the scratch database was not the last thing dealt with:\n%s", all)
	}
}

// A dump that restores into nothing is the failure this exists to catch. It has
// to be reported, not counted as a pass because no command errored.
func TestVerify_FailsWhenTheRestoredDatabaseIsEmpty(t *testing.T) {
	scriptedRuns(t, []string{"", "", "0"}, nil)

	res, err := verifyWith(verifySpec(), managedConn(), "acme_verify_1", strings.NewReader("-- dump"))
	if err == nil {
		t.Fatal("a dump that restored no tables was reported as verified")
	}
	if res.Tables != 0 {
		t.Errorf("tables = %d", res.Tables)
	}
}

// The scratch database is torn down even when the load fails. Leaving it behind
// would fill the server with half-restored copies, one per failed check.
func TestVerify_TearsDownEvenWhenTheLoadFails(t *testing.T) {
	runs := scriptedRuns(t, []string{"", "", ""}, map[int]error{1: errors.New("exit status 1")})

	_, err := verifyWith(verifySpec(), managedConn(), "acme_verify_1", strings.NewReader("-- dump"))
	if err == nil {
		t.Fatal("a failed load was reported as verified")
	}
	// Which failure it was matters. Carrying on to the count and reporting
	// whatever that says would hide the real reason behind a confusing one.
	if !strings.Contains(err.Error(), "would not load") {
		t.Errorf("error = %q, does not say the load was what failed", err)
	}
	last := strings.Join((*runs)[len(*runs)-1], " ")
	if !strings.Contains(last, "DROP DATABASE") {
		t.Errorf("the scratch database was left behind after a failed load:\n%s", last)
	}
}

// The administrator's password must not reach the error text, which ends up in
// the panel, the audit log and a screenshot of either.
func TestVerify_KeepsTheAdminPasswordOutOfTheError(t *testing.T) {
	scriptedRuns(t, nil, map[int]error{0: errors.New("exit status 1")})

	_, err := verifyWith(verifySpec(), managedConn(), "acme_verify_1", strings.NewReader("-- dump"))
	if err == nil {
		t.Fatal("a failed create was reported as verified")
	}
	if strings.Contains(err.Error(), "adminpassword") {
		t.Errorf("the administrator's password is in %q", err)
	}
}

// A scratch name is generated, never taken from anywhere a person typed, and it
// has to be one the engine will accept and nothing else could collide with.
func TestScratchName_IsAnIdentifierAndUnique(t *testing.T) {
	a := ScratchName("acme")
	b := ScratchName("acme")
	if a == b {
		t.Error("two scratch names collided, so one verify could drop another's database")
	}
	for _, name := range []string{a, b} {
		if !strings.HasPrefix(name, "servlo_verify_") {
			t.Errorf("%q does not say what it is, so nobody finding it knows it is safe to drop", name)
		}
		if len(name) > 64 {
			t.Errorf("%q is %d characters, longer than an engine accepts", name, len(name))
		}
		for _, r := range name {
			if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				t.Errorf("%q contains %q, which is not an identifier character", name, r)
			}
		}
	}
}
