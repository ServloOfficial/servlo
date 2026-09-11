package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wpConfig = `<?php
define( 'DB_NAME', 'shop' );
define( 'DB_USER', 'shop' );
define('WP_DEBUG', false);

/* That's all, stop editing! */
require_once ABSPATH . 'wp-settings.php';
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wp-config.php")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The whole reason this exists: PHP reads every non-empty string as true, so a
// flag written as 'false' is a flag left on.
func TestApplyPhpConstLiterals_WritesTheBooleanNotTheWord(t *testing.T) {
	path := writeConfig(t, wpConfig)

	if err := ApplyPhpConstLiterals(path, map[string]string{"DISABLE_WP_CRON": "true"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "define( 'DISABLE_WP_CRON', true );") {
		t.Fatalf("the constant was not added as a literal:\n%s", body)
	}
	// Added above the marker, where a WordPress config expects it, not after
	// the require that ends the file.
	if strings.Index(string(body), "DISABLE_WP_CRON") > strings.Index(string(body), "That's all") {
		t.Errorf("the constant landed after wp-settings.php, where it does nothing:\n%s", body)
	}

	if err := ApplyPhpConstLiterals(path, map[string]string{"DISABLE_WP_CRON": "false"}); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	if strings.Contains(string(body), "'false'") {
		t.Errorf("the value was quoted, which PHP reads as true:\n%s", body)
	}
	if !strings.Contains(string(body), "define( 'DISABLE_WP_CRON', false );") {
		t.Errorf("the constant was not rewritten in place:\n%s", body)
	}
	if strings.Count(string(body), "DISABLE_WP_CRON") != 1 {
		t.Errorf("the constant was defined twice:\n%s", body)
	}
}

// A string constant next door must not be turned into a literal on the way
// past, which would break the site's database credentials.
func TestApplyPhpConstLiterals_LeavesStringConstantsAlone(t *testing.T) {
	path := writeConfig(t, wpConfig)
	if err := ApplyPhpConstLiterals(path, map[string]string{"DISABLE_WP_CRON": "true"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "define( 'DB_NAME', 'shop' );") {
		t.Errorf("a string constant was rewritten:\n%s", body)
	}
}

func TestReadPhpConstLiterals(t *testing.T) {
	path := writeConfig(t, wpConfig)
	got, err := ReadPhpConstLiterals(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["WP_DEBUG"] != "false" {
		t.Errorf("WP_DEBUG = %q, want the literal false", got["WP_DEBUG"])
	}
	if got["DB_NAME"] != "'shop'" {
		t.Errorf("DB_NAME = %q, want the value exactly as written", got["DB_NAME"])
	}
}

// Removing a constant is how a setting goes back to the framework's own
// default, rather than being pinned to whatever matches it today.
func TestRemovePhpConsts(t *testing.T) {
	path := writeConfig(t, wpConfig)
	if err := ApplyPhpConstLiterals(path, map[string]string{"DISABLE_WP_CRON": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := RemovePhpConsts(path, "DISABLE_WP_CRON"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "DISABLE_WP_CRON") {
		t.Errorf("the constant survived removal:\n%s", body)
	}
	if !strings.Contains(string(body), "define( 'DB_NAME', 'shop' );") {
		t.Errorf("removal took a neighbouring constant with it:\n%s", body)
	}
}

// A value goes into the config as a single-quoted PHP string, so an apostrophe
// in one closes the string and the rest of the line is syntax. That is not the
// setting failing, it is a parse error in wp-config.php: WordPress cannot load
// at all, so every page of the site is a 500 from the moment the operator
// pressed Save on the mail form.
//
// An apostrophe in an SMTP password is ordinary, and so is a backslash, which
// escapes whatever follows it in a PHP single-quoted string.
func TestApplyPhpConstUpdates_EscapesAValueThatWouldCloseTheString(t *testing.T) {
	path := writeConfig(t, wpConfig)

	const pass = `o'brien\pass`
	if err := ApplyPhpConstUpdates(path, map[string]string{"SMTP_PASS": pass}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `'o\'brien\\pass'`) {
		t.Fatalf("the value was written unescaped, so the file is not PHP any more:\n%s", body)
	}

	// And a second save has to find the value it wrote the first time rather
	// than reading the escaped quote as the end of the string.
	if err := ApplyPhpConstUpdates(path, map[string]string{"SMTP_PASS": "plain"}); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	if !strings.Contains(string(body), `define( 'SMTP_PASS', 'plain' );`) && !strings.Contains(string(body), `'SMTP_PASS', 'plain'`) {
		t.Fatalf("the second save did not replace the escaped value:\n%s", body)
	}
	if strings.Contains(string(body), `o\'brien`) {
		t.Fatalf("the old value is still in the file:\n%s", body)
	}
}

// A from name carrying a double quote reads back as nothing, because the
// pattern that found the value stopped at either quote whichever one opened it.
// The panel then shows the setting as empty over a config file that has it.
func TestReadPhpConst_ReadsAValueHoldingTheOtherQuote(t *testing.T) {
	path := writeConfig(t, wpConfig)
	if err := ApplyPhpConstUpdates(path, map[string]string{"SMTP_NAME": `Acme "Support" Team`}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPhpConst(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["SMTP_NAME"] != `Acme "Support" Team` {
		t.Errorf("read back %q, want the name it was given", got["SMTP_NAME"])
	}
}
