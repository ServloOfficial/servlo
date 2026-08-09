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
