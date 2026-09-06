package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The archive names a site this server does not have. That is the operator
// reaching for the wrong archive, and the message says what to do instead.
func TestSiteLookupFailure_SaysHowToRestoreAnUnknownSite(t *testing.T) {
	err := siteLookupFailure("shop", config.ErrSiteNotFound)
	if err == nil {
		t.Fatal("a failed lookup was reported as success")
	}
	if !strings.Contains(err.Error(), "--into") {
		t.Errorf("error %q does not say how to restore it somewhere", err)
	}
}

// The registry itself could not be read. Reporting that as a site this server
// does not have sends the operator looking at their archives instead of at the
// permissions on their own config, which is how a rebuild failure stayed
// misdiagnosed through four CI runs.
func TestSiteLookupFailure_KeepsTheReasonTheRegistryCouldNotBeRead(t *testing.T) {
	cause := errors.New("permission denied")

	err := siteLookupFailure("shop", cause)
	if err == nil {
		t.Fatal("a failed lookup was reported as success")
	}
	if !errors.Is(err, cause) {
		t.Errorf("error %q does not carry the underlying cause", err)
	}
	if strings.Contains(err.Error(), "--into") {
		t.Errorf("error %q blames the archive for a registry that could not be read", err)
	}
}
