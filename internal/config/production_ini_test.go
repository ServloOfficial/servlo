package config

import (
	"os"
	"strings"
	"testing"
)

// The three settings CLAUDE.md 3.4 names as production defaults on every site.
func TestProductionBody_HasTheProductionDefaults(t *testing.T) {
	body := Body(true)
	for _, want := range []string{
		"display_errors = Off",
		"expose_php = Off",
		"opcache.enable = 1",
		"opcache.validate_timestamps = 0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the production ini does not set %q:\n%s", want, body)
		}
	}
}

// display_startup_errors is separate from display_errors in PHP and is missed
// often enough to be worth pinning: a startup error still reaches the visitor
// with only the first one turned off.
func TestProductionBody_AlsoHidesStartupErrors(t *testing.T) {
	if !strings.Contains(Body(true), "display_startup_errors = Off") {
		t.Errorf("startup errors are still shown in production:\n%s", Body(true))
	}
}

// Turning the mode off writes the development values rather than deleting the
// file. A file that disappears leaves the previous behaviour in place on a
// container that has not been rebuilt, so the switch would look like it worked
// and change nothing.
func TestDevelopmentBody_RestoresTheDevelopmentValues(t *testing.T) {
	body := Body(false)
	for _, want := range []string{
		"display_errors = On",
		"opcache.validate_timestamps = 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the development ini does not set %q:\n%s", want, body)
		}
	}
}

// The filename has to sort before the shared and per-version files, since
// conf.d loads alphabetically and the last value read wins. Sorting after them
// would make servlo silently override whatever an operator set by hand.
func TestProductionIniSortsBeforeTheOperatorsOwnFiles(t *testing.T) {
	for _, later := range []string{"95-shared.ini", "98-user.ini"} {
		if productionIniName >= later {
			t.Errorf("%s does not sort before %s, so it would override the operator's own settings",
				productionIniName, later)
		}
	}
}

func TestWriteProductionIni_RoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := WriteProductionIni(true); err != nil {
		t.Fatalf("WriteProductionIni: %v", err)
	}
	data, err := readFile(ProductionIniFile())
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !strings.Contains(data, "display_errors = Off") {
		t.Errorf("what landed on disk is not the production body:\n%s", data)
	}

	if err := WriteProductionIni(false); err != nil {
		t.Fatalf("WriteProductionIni(false): %v", err)
	}
	data, _ = readFile(ProductionIniFile())
	if !strings.Contains(data, "display_errors = On") {
		t.Errorf("turning the mode off did not restore the development body:\n%s", data)
	}
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
