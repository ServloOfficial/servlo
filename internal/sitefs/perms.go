package sitefs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Fixing permissions.
//
// "Fix permissions" is the button an operator reaches for after moving a site
// onto the machine with tar, or after an upload landed with whatever umask the
// browser's process had. The temptation is chmod -R 755, and that is precisely
// the wrong tool: it makes every .env world-readable, and on the way past it
// strips the execute bit off artisan and every binary in vendor/bin.
//
// So this is four named rules with a reason each, and the panel shows them
// before anything is applied. Nothing is recursive-and-uniform; every path is
// classified, and a path no rule claims is left exactly as it was.

// The rule names, which are also what the panel labels each row with.
const (
	RuleDirectories = "directories"
	RuleFiles       = "files"
	RuleExecutables = "executables"
	RuleSecrets     = "secrets"
)

const (
	dirMode    fs.FileMode = 0o755
	fileMode   fs.FileMode = 0o644
	execMode   fs.FileMode = 0o755
	secretMode fs.FileMode = 0o600
)

// maxWalkEntries bounds the pass. A site with a node_modules tree runs to
// hundreds of thousands of paths, and an operator who clicks a button deserves
// an answer rather than a spinner.
const maxWalkEntries = 200000

// Rule is one mode servlo will set, why, and how much of the site it touches.
type Rule struct {
	Name   string      `json:"name"`
	Mode   fs.FileMode `json:"-"`
	Octal  string      `json:"octal"`
	Reason string      `json:"reason"`
	// Count is how many paths this rule claims, whether or not they already
	// have the mode.
	Count int `json:"count"`
	// Changes is how many of those are actually wrong today, which is the
	// number that answers "will this do anything".
	Changes int `json:"changes"`
	// Samples are a few of the paths that would change, so the plan is
	// something an operator can check rather than a number to trust.
	Samples []string `json:"samples,omitempty"`
}

// Plan is what "fix permissions" would do.
type Plan struct {
	Rules   []Rule `json:"rules"`
	Changes int    `json:"changes"`
	// Skipped counts what no rule claims: symlinks, sockets, and everything
	// under .git.
	Skipped int `json:"skipped"`
	// Truncated says the site is larger than servlo will walk in one pass.
	Truncated bool `json:"truncated"`
}

// Result is what it did.
type Result struct {
	Plan
	// Applied is how many paths were chmodded.
	Applied int `json:"applied"`
	// Failed names paths the chmod would not take, which on this machine
	// is usually something owned by another account.
	Failed []string `json:"failed,omitempty"`
}

const maxSamples = 5

// PermissionPlan classifies the site without changing anything.
func (r Root) PermissionPlan() (Plan, error) {
	plan, _, err := r.classify()
	return plan, err
}

// ApplyPermissions sets the modes the plan named, and only those.
func (r Root) ApplyPermissions() (Result, error) {
	plan, changes, err := r.classify()
	if err != nil {
		return Result{}, err
	}
	result := Result{Plan: plan}
	for target, mode := range changes {
		if err := os.Chmod(target, mode); err != nil {
			if len(result.Failed) < maxSamples {
				result.Failed = append(result.Failed, r.Rel(target))
			}
			continue
		}
		result.Applied++
	}
	return result, nil
}

// classify walks the site once and returns the plan plus the exact chmods it
// implies. One walk for both, so the plan an operator approved and the chmods
// that follow cannot be produced by two different pieces of logic.
func (r Root) classify() (Plan, map[string]fs.FileMode, error) {
	rules := map[string]*Rule{
		RuleDirectories: {Name: RuleDirectories, Mode: dirMode, Reason: "nginx and PHP-FPM have to traverse the tree to serve anything under it"},
		RuleFiles:       {Name: RuleFiles, Mode: fileMode, Reason: "readable by the server, writable only by the account that owns the site"},
		RuleExecutables: {Name: RuleExecutables, Mode: execMode, Reason: "a file that is already executable stays executable, so artisan and vendor/bin keep working"},
		RuleSecrets:     {Name: RuleSecrets, Mode: secretMode, Reason: "credentials and private keys are readable only by the account that owns the site"},
	}
	order := []string{RuleDirectories, RuleFiles, RuleExecutables, RuleSecrets}

	plan := Plan{}
	changes := map[string]fs.FileMode{}
	walked := 0

	err := filepath.WalkDir(r.path, func(target string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory servlo cannot read is reported as skipped rather than
			// aborting the pass: one unreadable corner should not stop the
			// other nine tenths of the site being fixed.
			plan.Skipped++
			return nil //nolint:nilerr
		}
		walked++
		if walked > maxWalkEntries {
			plan.Truncated = true
			return filepath.SkipAll
		}
		// Git keeps its own modes, and a chmod pass over .git is how a
		// repository ends up with hooks that no longer run.
		if d.IsDir() && d.Name() == ".git" {
			plan.Skipped++
			return filepath.SkipDir
		}
		if target == r.path {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			plan.Skipped++
			return nil
		}
		rule := ruleFor(info)
		if rule == "" {
			// Symlinks, sockets, fifos, devices. Chmod follows a symlink, so
			// applying a mode to one would apply it to whatever it points at,
			// which is the one place this pass could leave the site.
			plan.Skipped++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		r.record(rules[rule], info, target, changes)
		return nil
	})
	if err != nil {
		return Plan{}, nil, err
	}

	for _, name := range order {
		rule := *rules[name]
		// Rendered from the same value the chmod uses, so the number the panel
		// prints cannot describe a mode this does not set.
		rule.Octal = formatOctal(rule.Mode)
		plan.Rules = append(plan.Rules, rule)
		plan.Changes += rule.Changes
	}
	return plan, changes, nil
}

// record adds one path to its rule and, when the mode is wrong, to the chmod
// set.
func (r Root) record(rule *Rule, info fs.FileInfo, target string, changes map[string]fs.FileMode) {
	rule.Count++
	if info.Mode().Perm() == rule.Mode {
		return
	}
	rule.Changes++
	if len(rule.Samples) < maxSamples {
		rule.Samples = append(rule.Samples, r.Rel(target))
	}
	changes[target] = rule.Mode
}

// ruleFor decides which rule claims a path, or none.
//
// Order matters: a secret that happens to carry an execute bit is a secret, not
// an executable. Nothing about a .env wants 0755.
func ruleFor(info fs.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return ""
	case info.IsDir():
		return RuleDirectories
	case !info.Mode().IsRegular():
		return ""
	case isSecretName(info.Name()):
		return RuleSecrets
	case info.Mode().Perm()&0o100 != 0:
		return RuleExecutables
	default:
		return RuleFiles
	}
}

// secretSuffixes are the extensions that are a private key by convention.
var secretSuffixes = []string{".pem", ".key", ".p12", ".pfx"}

// secretNames are the exact filenames that hold credentials.
var secretNames = []string{".env", ".htpasswd", "id_rsa", "id_ed25519", "id_ecdsa"}

// isSecretName reports whether a filename is one servlo will not leave
// group-readable. CLAUDE.md section 3.7 already fixes .env at 0600; the rest of
// the list is the same argument applied to the other things an operator drops
// into a site directory.
func isSecretName(name string) bool {
	lower := strings.ToLower(name)
	for _, exact := range secretNames {
		if lower == exact {
			return true
		}
	}
	// .env.production, .env.local: the same file with a suffix.
	if strings.HasPrefix(lower, ".env.") {
		return true
	}
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func formatOctal(mode fs.FileMode) string {
	const digits = "01234567"
	perm := mode.Perm()
	return string([]byte{'0', digits[(perm>>6)&7], digits[(perm>>3)&7], digits[perm&7]})
}
