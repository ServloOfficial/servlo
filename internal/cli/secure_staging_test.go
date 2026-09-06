package cli

import (
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// --staging sets the install's authority rather than overriding one issuance.
// A per-run override would leave the renewal scanner pointed at production, so
// the staging certificate an operator just tested with would be silently
// replaced by a production one at the 30-day mark, spending the production rate
// limit they were trying to protect.
func TestApplyStagingFlag_PersistsSoRenewalAgrees(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := applyStagingFlag(true); err != nil {
		t.Fatalf("applyStagingFlag: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, staging := cfg.ACMESettings(); !staging {
		t.Error("--staging did not record the staging authority")
	}

	if err := applyStagingFlag(false); err != nil {
		t.Fatalf("applyStagingFlag(false): %v", err)
	}
	cfg, err = config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, staging := cfg.ACMESettings(); staging {
		t.Error("--staging=false did not return the install to production")
	}
}

// The flag has to be reachable from the command, since a setting nobody can
// reach from the CLI is a setting an operator edits by hand under pressure.
func TestSecureCmd_OffersTheStagingFlag(t *testing.T) {
	cmd := NewSecureCmd()
	flag := cmd.Flags().Lookup("staging")
	if flag == nil {
		t.Fatal("servlo secure has no --staging flag")
	}
	if flag.Usage == "" {
		t.Error("--staging has no help text saying what it changes")
	}
}
