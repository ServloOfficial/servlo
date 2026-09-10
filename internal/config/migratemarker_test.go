package config

import (
	"os"
	"path/filepath"
	"testing"
)

// storeFramework reads a definition straight out of the store, the way
// deployprofile_test does, so these assert on what ships rather than on a
// fixture that could drift from it.
func storeFramework(t *testing.T, file string) Framework {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "stores", "frameworks", file))
	if err != nil {
		t.Fatal(err)
	}
	return parseFramework(t, string(body))
}

// The deploy script is the operator's, editable, and servlo never rewrites it.
// So recognising a migration in it cannot depend on the operator spelling the
// command the way the shipped script happens to.
//
// Drupal is where this showed: the definition declared `updb`, drush's alias,
// and the definition's own setup step calls the same update `updatedb`, which is
// the canonical name and what Drupal's documentation uses. A deploy script
// saying `drush updatedb -y` therefore ran without the database backup that
// CLAUDE.md section 3.5 promises before any migrating deploy, and the comment
// beside the declaration said the two spell it the same way.
//
// Getting this wrong in the other direction costs an unnecessary dump before a
// deploy that did not need one. Getting it wrong this way costs the client's
// database, so every marker here is the distinctive part of the command rather
// than one particular way of invoking it.
func TestScriptMigrates_RecognisesHowAnOperatorWouldWriteIt(t *testing.T) {
	cases := []struct {
		file   string
		script string
	}{
		{"drupal/11.yaml", "php vendor/drush/drush/drush.php updatedb --yes"},
		{"drupal/11.yaml", "drush updb -y"},
		{"drupal/10.yaml", "drush updatedb -y"},
		{"laravel/12.yaml", "php artisan migrate --force"},
		{"laravel/12.yaml", "./artisan migrate --force"},
		{"laravel/11.yaml", "$PHP_BIN artisan migrate --force"},
		{"symfony/7.yaml", "php bin/console doctrine:migrations:migrate --no-interaction"},
		{"symfony/7.yaml", "bin/console d:m:m -n"},
		{"cakephp/5.yaml", "bin/cake migrations migrate"},
		{"codeigniter/4.yaml", "php spark migrate"},
		{"magento/2.yaml", "bin/magento setup:upgrade"},
		{"magento/2.yaml", "magento setup:upgrade --keep-generated"},
		{"statamic/6.yaml", "php artisan migrate --force"},
	}
	for _, tc := range cases {
		fw := storeFramework(t, tc.file)
		if !fw.ScriptMigrates(tc.script) {
			t.Errorf("%s did not recognise a migration in %q, so a deploy running it takes no database backup first",
				tc.file, tc.script)
		}
	}
}

// A framework with no migrations still says no, and an ordinary deploy line is
// not mistaken for one.
func TestScriptMigrates_StillSaysNoWhenThereIsNoMigration(t *testing.T) {
	fw := storeFramework(t, "laravel/12.yaml")
	for _, script := range []string{
		"composer install --no-dev",
		"npm run build",
		"# php artisan migrate",
		"",
	} {
		if fw.ScriptMigrates(script) {
			t.Errorf("%q was treated as a migration", script)
		}
	}

	grav := storeFramework(t, "grav/2.yaml")
	if grav.ScriptMigrates("php bin/gpm selfupgrade") {
		t.Error("a framework declaring no migration reported one")
	}
}
