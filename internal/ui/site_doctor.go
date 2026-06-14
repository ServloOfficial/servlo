package ui

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
)

// Doctor check statuses, mirroring the MCP doctor's check shape so the two
// diagnostics read consistently. "unknown" covers a check lerd couldn't run
// (e.g. the app is down), which is distinct from a genuine pass or failure.
const (
	doctorOK      = "ok"
	doctorWarn    = "warn"
	doctorFail    = "fail"
	doctorUnknown = "unknown"
)

// migrateStatusTimeout bounds the one container exec the doctor makes. Booting
// Laravel + reaching the DB is usually sub-second, but a wedged app or an
// unreachable DB shouldn't hang the panel — it degrades to an "unknown" check.
const migrateStatusTimeout = 25 * time.Second

// DoctorCheck is one app-level health finding for a site. Fix, when set, names
// a command from the site's command set (GET /api/sites/{domain}/commands) that
// the UI can run through the existing command runner to resolve the finding —
// so the doctor never grows its own mutation endpoints.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// DoctorResponse is the body of GET /api/sites/{domain}/doctor.
type DoctorResponse struct {
	Checks   []DoctorCheck `json:"checks"`
	Failures int           `json:"failures"`
	Warnings int           `json:"warnings"`
}

func (d *DoctorResponse) add(c DoctorCheck) {
	switch c.Status {
	case doctorFail:
		d.Failures++
	case doctorWarn:
		d.Warnings++
	}
	d.Checks = append(d.Checks, c)
}

// doctorRoute handles GET /api/sites/{domain}/doctor, returning the app-level
// health checks for a Laravel site. Loopback-only: the migrations check execs
// `php artisan migrate:status` in the site's container, the same trust level as
// the command runner. Returns true when it owns the request.
func doctorRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) != 1 || rest[0] != "doctor" {
		return false
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": "site not found: " + domain})
		return true
	}
	// The checks are Laravel-flavoured (APP_KEY, migrations, storage:link), so
	// don't run them against other frameworks — the UI only shows the tab for
	// Laravel sites, but guard the endpoint too.
	if site.Framework != "laravel" {
		writeJSON(w, DoctorResponse{Checks: []DoctorCheck{}})
		return true
	}
	branch := r.URL.Query().Get("branch")
	path := site.Path
	if branch != "" {
		// An unresolved branch must not fall back to the parent checkout, or the
		// doctor would diagnose the main site's .env and database while the UI
		// thinks it's looking at the worktree. Refuse, as the command runner does.
		wt := resolveSitePath(site, branch)
		if wt == "" {
			writeJSON(w, map[string]any{"error": "unknown worktree branch: " + branch})
			return true
		}
		path = wt
	}
	// Freshly added worktrees don't carry .env (it's gitignored), so materialise
	// it first — otherwise every file check reads a missing .env and reports a
	// healthy worktree as broken. No-op for the parent and idempotent.
	ensureWorktreeEnvIfBranch(site, branch)
	writeJSON(w, runSiteDoctor(r.Context(), path))
	return true
}

// runSiteDoctor builds the doctor report for the project at path. The cheap
// checks read files only; the migrations check is the one that touches the
// container. Checks that don't apply to a project (no .env.example, no public
// disk) are omitted rather than reported as passing.
func runSiteDoctor(ctx context.Context, path string) DoctorResponse {
	resp := DoctorResponse{Checks: []DoctorCheck{}}
	envPath := filepath.Join(path, ".env")

	resp.add(checkAppKey(envPath))
	if c, ok := checkEnvDrift(path, envPath); ok {
		resp.add(c)
	}
	resp.add(checkAppDebug(envPath))
	if c, ok := checkStorageLink(path); ok {
		resp.add(c)
	}
	resp.add(checkMigrations(ctx, path))
	return resp
}

// checkAppKey fails when APP_KEY is unset, which breaks encryption, signed
// URLs, and session cookies.
func checkAppKey(envPath string) DoctorCheck {
	if strings.TrimSpace(envfile.ReadKey(envPath, "APP_KEY")) == "" {
		return DoctorCheck{
			Name:   "app_key",
			Status: doctorFail,
			Detail: "APP_KEY is empty — encryption, signed URLs, and sessions won't work until it's set.",
			Fix:    "key:generate",
		}
	}
	return DoctorCheck{Name: "app_key", Status: doctorOK}
}

// checkEnvDrift warns when .env.example declares keys the project's .env is
// missing — the classic "pulled main, app breaks on a new env var" trap. Only
// key names are surfaced, never values, so it's safe to return over the wire.
// Skipped (ok=false) when there's no .env.example to compare against.
func checkEnvDrift(path, envPath string) (DoctorCheck, bool) {
	examplePath := filepath.Join(path, ".env.example")
	if _, err := os.Stat(examplePath); err != nil {
		return DoctorCheck{}, false
	}
	exampleKeys, err := envfile.ReadKeys(examplePath)
	if err != nil {
		return DoctorCheck{}, false
	}
	have := map[string]bool{}
	if envKeys, err := envfile.ReadKeys(envPath); err == nil {
		for _, k := range envKeys {
			have[k] = true
		}
	}
	var missing []string
	for _, k := range exampleKeys {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return DoctorCheck{Name: "env_drift", Status: doctorOK}, true
	}
	// Not every missing key matters: Laravel only breaks on keys read with no
	// default (env('KEY') vs env('KEY', 'fallback')). Split on that signal so
	// the warning fires only for keys the app genuinely needs.
	required, optional := classifyMissingEnvKeys(path, missing)
	if len(required) == 0 {
		return DoctorCheck{
			Name:   "env_drift",
			Status: doctorOK,
			Detail: fmt.Sprintf("%d key(s) in .env.example aren't set, but the app reads them with defaults, so this is fine.", len(optional)),
		}, true
	}
	detail := fmt.Sprintf("%d required key(s) missing from .env: %s", len(required), summariseKeys(required, 12))
	if len(optional) > 0 {
		detail += fmt.Sprintf(" (%d more have code defaults and were skipped)", len(optional))
	}
	return DoctorCheck{
		Name:   "env_drift",
		Status: doctorWarn,
		Detail: detail,
	}, true
}

// envCallRe matches a Laravel env('KEY'...) read, capturing the key and the
// char after it: ',' means a default follows (optional), ')' means none does
// (required). The \b stops getenv( and app_env( from matching.
var envCallRe = regexp.MustCompile(`\benv\(\s*['"]([A-Za-z0-9_]+)['"]\s*([,)])`)

// envKeyUsage records how the project's code reads one env key: whether it's
// ever read without a default (so a missing value really breaks something).
type envKeyUsage struct {
	noDefault bool
}

// scanEnvUsage walks the project's PHP for env() reads (skipping vendor and
// build dirs) and returns per-key usage plus the total calls found, so the
// caller can fall back to warning on everything when the scan finds nothing.
func scanEnvUsage(path string) (map[string]envKeyUsage, int) {
	usage := map[string]envKeyUsage{}
	total := 0
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "node_modules", ".git", "storage":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".php") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range envCallRe.FindAllStringSubmatch(string(data), -1) {
			total++
			u := usage[m[1]]
			if m[2] == ")" {
				u.noDefault = true
			}
			usage[m[1]] = u
		}
		return nil
	})
	return usage, total
}

// classifyMissingEnvKeys splits missing keys into required (read with no
// default, or VITE_-prefixed so the frontend build needs them) and optional
// (read only with defaults, or unreferenced). No env() calls found, all required.
func classifyMissingEnvKeys(path string, missing []string) (required, optional []string) {
	usage, total := scanEnvUsage(path)
	for _, k := range missing {
		switch {
		case total == 0:
			required = append(required, k)
		case strings.HasPrefix(k, "VITE_"):
			required = append(required, k)
		case usage[k].noDefault:
			required = append(required, k)
		default:
			optional = append(optional, k)
		}
	}
	return required, optional
}

// summariseKeys joins keys for a detail line, capping the list so a project
// with dozens of missing keys doesn't produce a runaway message.
func summariseKeys(keys []string, max int) string {
	if len(keys) <= max {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:max], ", ") + fmt.Sprintf(", +%d more", len(keys)-max)
}

// checkAppDebug warns about the production footgun of APP_DEBUG=true while
// APP_ENV=production, which leaks stack traces. Plain local dev (APP_ENV=local
// with debug on) is expected and passes quietly.
func checkAppDebug(envPath string) DoctorCheck {
	env := strings.ToLower(strings.TrimSpace(envfile.ReadKey(envPath, "APP_ENV")))
	debug := strings.ToLower(strings.TrimSpace(envfile.ReadKey(envPath, "APP_DEBUG")))
	debugOn := debug == "true" || debug == "1" || debug == "on" || debug == "yes"
	if env == "production" && debugOn {
		return DoctorCheck{
			Name:   "app_debug",
			Status: doctorWarn,
			Detail: "APP_DEBUG is on while APP_ENV=production — stack traces and config will leak. Turn debug off.",
		}
	}
	return DoctorCheck{Name: "app_debug", Status: doctorOK}
}

// checkStorageLink warns when a project that uses the public disk
// (storage/app/public exists) is missing its public/storage symlink, so served
// uploads 404. Skipped (ok=false) for apps with no public disk or no public/
// dir, where the symlink is irrelevant.
func checkStorageLink(path string) (DoctorCheck, bool) {
	link := filepath.Join(path, "public", "storage")
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return DoctorCheck{Name: "storage_link", Status: doctorOK}, true
	}
	if info, err := os.Stat(filepath.Join(path, "storage", "app", "public")); err != nil || !info.IsDir() {
		return DoctorCheck{}, false
	}
	if info, err := os.Stat(filepath.Join(path, "public")); err != nil || !info.IsDir() {
		return DoctorCheck{}, false
	}
	return DoctorCheck{
		Name:   "storage_link",
		Status: doctorWarn,
		Detail: "public/storage symlink is missing — files on the public disk won't be web-accessible.",
		Fix:    "storage:link",
	}, true
}

// checkMigrations execs `php artisan migrate:status` in the site's container.
// It fails on pending migrations, passes when all have run, and degrades to
// "unknown" when the command can't run (app down, DB unreachable) so a wedged
// app never turns the whole panel into an error.
func checkMigrations(ctx context.Context, path string) DoctorCheck {
	cctx, cancel := context.WithTimeout(ctx, migrateStatusTimeout)
	defer cancel()
	out, exit, err := runArtisanCapture(cctx, path, "php artisan migrate:status")
	if err != nil || exit != 0 {
		return DoctorCheck{
			Name:   "migrations",
			Status: doctorUnknown,
			Detail: "Couldn't read migration status — the app may be down or the database unreachable.",
		}
	}
	if migrationsPending(out) {
		return DoctorCheck{
			Name:   "migrations",
			Status: doctorFail,
			Detail: "There are pending migrations — run migrate to apply them.",
			Fix:    "migrate",
		}
	}
	return DoctorCheck{Name: "migrations", Status: doctorOK}
}

// migrationsPending reports whether `migrate:status` output lists any not-yet-
// run migration. Laravel marks those rows "Pending" across the supported
// versions; the header carries no such token.
func migrationsPending(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Pending") {
			return true
		}
	}
	return false
}

// runArtisanCapture runs a shell command in cwd with lerd's bin shims on PATH
// (so `php` resolves to the container shim under launchd's restricted PATH),
// mirroring the command runner. Returns combined output and the exit code; a
// non-ExitError (couldn't even start) comes back as exit -1 with the error.
func runArtisanCapture(ctx context.Context, cwd, command string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	path := config.BinDir()
	if existing := os.Getenv("PATH"); existing != "" {
		path += string(os.PathListSeparator) + existing
	}
	cmd.Env = append(os.Environ(), "PATH="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode(), nil
		}
		return string(out), -1, err
	}
	return string(out), 0, nil
}
