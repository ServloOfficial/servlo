package siteimport

import (
	"github.com/ServloOfficial/servlo/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A dump that will not load is worth finding out before a half-imported site
// exists on the server.
func TestReadableDump_RefusesWhatWillNotLoad(t *testing.T) {
	dir := t.TempDir()

	if err := readableDump(filepath.Join(dir, "nothing.sql")); err == nil {
		t.Error("a dump that is not there was accepted")
	}
	if err := readableDump(dir); err == nil {
		t.Error("a directory was accepted as a dump")
	}
	if err := readableDump(write(t, filepath.Join(dir, "empty.sql"), "")); err == nil {
		t.Error("an empty file was accepted as a dump")
	}
}

// A compressed dump is the commonest thing somebody has, and the commonest
// thing handed over by mistake, because the name looks right and the content is
// not what any client tool will read.
func TestReadableDump_NamesCompressionRatherThanFailingLater(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"acme.sql.gz", "acme.sql.zip", "acme.sql.bz2"} {
		err := readableDump(write(t, filepath.Join(dir, name), "not really compressed but named so"))
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "compressed") {
			t.Errorf("%s was refused for the wrong reason: %v", name, err)
		}
	}
}

func TestReadableDump_AcceptsAPlainDump(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "acme.sql"), "CREATE TABLE users (id int);\n")
	if err := readableDump(path); err != nil {
		t.Errorf("a plain dump was refused: %v", err)
	}
}

// Nothing is registered for a directory that is not there. The alternative is a
// site in the registry pointing at a path somebody has not uploaded yet, which
// serves 404 and looks like servlo's fault.
func TestImport_RefusesADirectoryThatIsNotThere(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))

	if _, err := Import(Options{Path: filepath.Join(root, "gone"), Domain: "acme.example"}); err == nil {
		t.Error("a site was imported from a directory that does not exist")
	}
	if _, err := Import(Options{Path: "", Domain: "acme.example"}); err == nil {
		t.Error("a site was imported from nowhere")
	}
}

// A bad dump is refused before the site is created, not after, so a failed
// import leaves nothing behind to clean up.
func TestImport_ChecksTheDumpBeforeRegisteringAnything(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	site := filepath.Join(root, "acme")
	write(t, filepath.Join(site, "index.php"), "<?php\n")

	_, err := Import(Options{Path: site, Domain: "acme.example", Dump: filepath.Join(root, "missing.sql")})
	if err == nil {
		t.Fatal("an import with a missing dump reported success")
	}
	if _, statErr := os.Stat(filepath.Join(root, "data", "servlo", "sites.yaml")); statErr == nil {
		t.Error("a site was registered even though the import was refused")
	}
}

// An imported site has to be in the registry, and it was not in it at all.
//
// Import writes the artifacts through siteops.FinishLink — pool, vhost, quadlet
// — but adding the site to sites.yaml happens a layer up in linker.Apply, and
// this path does not go through linker. So `servlo import` produced a site
// nginx served and nothing else knew about: absent from `servlo sites`, skipped
// by backups and cron, and invisible to `servlo secure`, which is exactly what
// the command's own last line tells the operator to run next.
func TestImport_PutsTheSiteInTheRegistry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	site := filepath.Join(root, "acme")
	write(t, filepath.Join(site, "index.php"), "<?php\n")

	orig := finishLink
	finishLink = func(config.Site, string) error { return nil }
	t.Cleanup(func() { finishLink = orig })

	if _, err := Import(Options{Path: site, Domain: "acme.example"}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := config.FindSiteByDomain("acme.example")
	if err != nil {
		t.Fatalf("the imported site is not in the registry, so nothing but nginx knows it exists: %v", err)
	}
	if got.Path != site {
		t.Errorf("the registered site points at %q, want %q", got.Path, site)
	}
}
