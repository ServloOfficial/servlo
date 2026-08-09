package serviceops

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// writeCustomServiceBeside adds a second service to the config directory a test
// is already using. writeCustomService starts a fresh one each call, which would
// take the first service back out again.
func writeCustomServiceBeside(t *testing.T, name, body string) {
	t.Helper()
	dir := filepath.Join(config.ConfigDir(), "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The story in one test: install a database, get its admin UI, without the
// operator being asked a second time.
func TestAdminUIsFor_MySQLBringsPhpMyAdmin(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")

	if got := AdminUIsFor("mysql"); !slices.Contains(got, "phpmyadmin") {
		t.Errorf("AdminUIsFor(mysql) = %v, want phpmyadmin among them", got)
	}
}

// MariaDB is a different preset with a different family, and phpMyAdmin reaches
// it through the admin_for it already declares rather than through a list of
// engine names in Go.
func TestAdminUIsFor_MariaDBBringsPhpMyAdminThroughItsFamily(t *testing.T) {
	writeCustomService(t, "mariadb-11-8", "name: mariadb-11-8\nimage: docker.io/library/mariadb:11.8\nfamily: mariadb\nenv_role: mysql\npreset: mariadb\n")

	if got := AdminUIsFor("mariadb-11-8"); !slices.Contains(got, "phpmyadmin") {
		t.Errorf("AdminUIsFor(mariadb-11-8) = %v, want phpmyadmin", got)
	}
}

func TestAdminUIsFor_PostgresBringsPgAdmin(t *testing.T) {
	writeCustomService(t, "postgres", "name: postgres\nimage: docker.io/postgis/postgis:16-3.5-alpine\nfamily: postgres\n")

	got := AdminUIsFor("postgres")
	if !slices.Contains(got, "pgadmin") {
		t.Errorf("AdminUIsFor(postgres) = %v, want pgadmin", got)
	}
	if slices.Contains(got, "phpmyadmin") {
		t.Errorf("AdminUIsFor(postgres) = %v, which brought a MySQL UI along", got)
	}
}

// A second engine of the same family must not install a second copy of the same
// UI: one phpMyAdmin already lists every MySQL database this install reaches.
func TestAdminUIsFor_SkipsAUIAlreadyInstalled(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")
	writeCustomServiceBeside(t, "phpmyadmin", "name: phpmyadmin\nimage: docker.io/library/phpmyadmin:latest\npreset: phpmyadmin\n")

	if got := AdminUIsFor("mysql"); slices.Contains(got, "phpmyadmin") {
		t.Errorf("AdminUIsFor(mysql) = %v, want nothing to install when the UI is already there", got)
	}
}

// Redis is not a database with an admin UI in this sense, and installing it must
// not drag phpMyAdmin onto the machine.
func TestAdminUIsFor_UnrelatedServiceBringsNothingItShouldNot(t *testing.T) {
	writeCustomService(t, "gotenberg", "name: gotenberg\nimage: docker.io/gotenberg/gotenberg:8\n")

	if got := AdminUIsFor("gotenberg"); slices.Contains(got, "phpmyadmin") || slices.Contains(got, "pgadmin") {
		t.Errorf("AdminUIsFor(gotenberg) = %v", got)
	}
}

// The install runs the companion itself, so the operator gets the UI from the
// one action they took.
func TestInstallAdminUIs_InstallsTheCompanionAndSaysSo(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")
	var installed []string
	old := installWithoutAdminUIs
	installWithoutAdminUIs = func(name string) (*config.CustomService, error) {
		installed = append(installed, name)
		return &config.CustomService{Name: name}, nil
	}
	defer func() { installWithoutAdminUIs = old }()

	var events []PhaseEvent
	installAdminUIs("mysql", func(e PhaseEvent) { events = append(events, e) })

	if !slices.Contains(installed, "phpmyadmin") {
		t.Errorf("installed = %v, want phpmyadmin", installed)
	}
	var ready bool
	for _, e := range events {
		if e.Phase == "installing_admin_ui" && e.Dep == "phpmyadmin" && e.State == "ready" {
			ready = true
		}
	}
	if !ready {
		t.Errorf("the companion install was not reported: %+v", events)
	}
}

// A UI that will not install is a missing convenience, not a reason to unwind a
// database somebody is about to put a site on.
func TestInstallAdminUIs_AFailedCompanionIsReportedNotFatal(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")
	old := installWithoutAdminUIs
	installWithoutAdminUIs = func(string) (*config.CustomService, error) {
		return nil, fmt.Errorf("pull refused")
	}
	defer func() { installWithoutAdminUIs = old }()

	var failed bool
	installAdminUIs("mysql", func(e PhaseEvent) {
		if e.State == "failed" && e.Message == "pull refused" {
			failed = true
		}
	})

	if !failed {
		t.Error("a companion that would not install must be reported through the phase stream")
	}
}
