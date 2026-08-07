package appstore

import (
	"strings"
	"testing"
)

func TestGenerateSecrets_GivesEachDeclaredValueItsLength(t *testing.T) {
	app := App{Secrets: []Secret{{Name: "salt_a", Length: 64}, {Name: "salt_b", Length: 32}}}

	got, err := app.GenerateSecrets()
	if err != nil {
		t.Fatalf("GenerateSecrets: %v", err)
	}
	if len(got["salt_a"]) != 64 || len(got["salt_b"]) != 32 {
		t.Errorf("lengths = %d/%d, want 64/32", len(got["salt_a"]), len(got["salt_b"]))
	}
	if got["salt_a"] == got["salt_b"] {
		t.Error("two secrets came out identical")
	}
}

// A generated value is substituted into a config file the application executes.
// Render refuses a value carrying a quote, so generating one would be a bug
// waiting for a one-in-a-hundred install to find it.
func TestGenerateSecrets_NeverProducesAValueRenderWouldRefuse(t *testing.T) {
	app := App{Secrets: []Secret{{Name: "s", Length: 128}}}
	config := ConfigFile{Template: "x = '{{s}}';"}

	// Enough draws that a forbidden character in the alphabet would show up.
	for i := 0; i < 500; i++ {
		values, err := app.GenerateSecrets()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := config.Render(values); err != nil {
			t.Fatalf("a generated secret would break the config file: %v", err)
		}
	}
}

func TestSecretAlphabet_CarriesNothingThatEndsAString(t *testing.T) {
	if strings.ContainsAny(secretAlphabet, "'\"\\\n\r\x00") {
		t.Error("the alphabet can produce a value that breaks out of its quotes")
	}
}

// Two installs of the same app must not share a salt.
func TestGenerateSecrets_DiffersBetweenRuns(t *testing.T) {
	app := App{Secrets: []Secret{{Name: "s", Length: 64}}}
	first, _ := app.GenerateSecrets()
	second, _ := app.GenerateSecrets()

	if first["s"] == second["s"] {
		t.Error("two installs generated the same secret")
	}
}
