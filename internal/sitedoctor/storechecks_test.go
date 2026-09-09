package sitedoctor

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/stores"
)

// Every doctor check in the store has to be one this binary can actually run.
//
// runDeclaredCheck skips a type it does not know, on purpose: a store definition
// published after a binary was built has to be readable by that binary rather
// than breaking it. The cost of that tolerance is that a check misspelled in a
// definition shipped here is skipped in exactly the same silence. Nobody reports
// a check that never ran, so the site simply passes a thing that was never
// looked at, which is worse than the finding it was written to catch.
//
// So the store this repository ships is held to the stricter rule the tolerance
// cannot afford: every type is dispatched, and every check carries the fields
// its own evaluator reads. A new check type is added to both halves or to
// neither.
func TestStoreDoctorChecks_AreAllRunnableByThisBinary(t *testing.T) {
	for _, file := range frameworkFiles(t) {
		raw, ok := stores.Read(stores.Frameworks, file)
		if !ok {
			t.Fatalf("%s: not in the embedded store", file)
		}
		var fw config.Framework
		if err := yaml.Unmarshal(raw, &fw); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if fw.Doctor == nil {
			continue
		}
		for _, spec := range fw.Doctor.Checks {
			if spec.Name == "" {
				t.Errorf("%s: a doctor check has no name", file)
			}
			for _, missing := range missingFieldsFor(spec) {
				t.Errorf("%s: check %q (%s) %s", file, spec.Name, spec.Type, missing)
			}
		}
	}
}

// missingFieldsFor names what a check declares that its evaluator will not be
// able to use. The rules are read off the evaluators rather than invented: each
// one is a field the evaluator reads and cannot do without.
//
// A command check needs no fail_if_ at all. Magento's "installed" check is
// `test -f app/etc/config.php`, where the exit status is the whole answer, and
// checkCommand treats a non-zero exit with nothing named as the finding.
func missingFieldsFor(spec config.DoctorCheck) []string {
	switch spec.Type {
	case "env_key_set":
		if spec.EnvKey == "" {
			return []string{"names no env_key, so checkEnvKeySet reads nothing"}
		}
	case "env_combo":
		if len(spec.When) == 0 && len(spec.WarnIf) == 0 {
			return []string{"has neither when nor warn_if, so it fires on every site unconditionally"}
		}
	case "symlink":
		var missing []string
		if spec.Link == "" {
			missing = append(missing, "names no link")
		}
		if spec.Target == "" {
			missing = append(missing, "names no target, so checkSymlink skips it")
		}
		return missing
	case "command":
		if strings.TrimSpace(spec.Command) == "" {
			return []string{"names no command"}
		}
	default:
		return []string{"is a type runDeclaredCheck does not dispatch, so it never runs"}
	}
	return nil
}

// A fix has to name a command the framework declares, or the panel offers a
// button that resolves to nothing.
func TestStoreDoctorChecks_EveryFixNamesACommandTheFrameworkHas(t *testing.T) {
	for _, file := range frameworkFiles(t) {
		raw, _ := stores.Read(stores.Frameworks, file)
		var fw config.Framework
		if err := yaml.Unmarshal(raw, &fw); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if fw.Doctor == nil {
			continue
		}
		have := map[string]bool{}
		for _, c := range fw.Commands {
			have[c.Name] = true
		}
		for _, spec := range fw.Doctor.Checks {
			if spec.Fix == "" || have[spec.Fix] {
				continue
			}
			if _, universal := DoctorFixCommands[spec.Fix]; universal {
				continue
			}
			t.Errorf("%s: check %q offers the fix %q, which is not a command this framework declares", file, spec.Name, spec.Fix)
		}
	}
}

// frameworkFiles lists every framework definition in the embedded store, so a
// framework added later is held to these rules without anyone remembering to
// add it here.
func frameworkFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := fs.WalkDir(stores.FS(), "frameworks", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".yaml" {
			return nil
		}
		files = append(files, strings.TrimPrefix(p, "frameworks/"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no framework definitions found in the embedded store")
	}
	return files
}
