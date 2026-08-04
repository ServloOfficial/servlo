package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/mcp"
)

// TestEveryMCPToolIsDocumented guards against doc drift: the single canonical
// reference (aidocs/servlo-reference.md, embedded as servloReference and shared by
// every client) is hand-maintained, not generated from the tool list, so a
// newly registered MCP tool must be added by hand. This fails until that
// happens. Names are matched backtick-wrapped to avoid substring false
// positives (e.g. "node" inside "site_node").
func TestEveryMCPToolIsDocumented(t *testing.T) {
	for _, name := range mcp.ToolNames() {
		token := "`" + name + "`"
		if !strings.Contains(servloReference, token) {
			t.Errorf("tool %q is missing from aidocs/servlo-reference.md", name)
		}
	}
}

// TestEveryMCPActionIsDocumented is the same guard one level down. Tool names
// alone drifted clean while `tls_renew` and `preset_search` shipped documented
// nowhere, so an assistant reading the reference could not know they existed.
// Actions are matched backtick-wrapped, the form the reference lists them in.
func TestEveryMCPActionIsDocumented(t *testing.T) {
	for tool, actions := range mcp.ToolActions() {
		for _, action := range actions {
			if !strings.Contains(servloReference, "`"+action+"`") {
				t.Errorf("action %q of tool %q is missing from aidocs/servlo-reference.md", action, tool)
			}
		}
	}
}

func TestWriteGlobalAISkills_writesAllThreeFiles(t *testing.T) {
	home := t.TempDir()

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("WriteGlobalAISkills: %v", err)
	}

	expect := []string{
		filepath.Join(home, ".claude", "skills", "servlo", "SKILL.md"),
		filepath.Join(home, ".cursor", "rules", "servlo.mdc"),
		filepath.Join(home, ".junie", "guidelines.md"),
	}
	for _, path := range expect {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", path)
		}
	}

	skill, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "servlo", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(skill) != renderClaudeSkill() {
		t.Errorf("SKILL.md content does not match renderClaudeSkill()")
	}

	rules, err := os.ReadFile(filepath.Join(home, ".cursor", "rules", "servlo.mdc"))
	if err != nil {
		t.Fatalf("read servlo.mdc: %v", err)
	}
	if string(rules) != renderCursorRules() {
		t.Errorf("servlo.mdc content does not match renderCursorRules()")
	}

	guidelines, err := os.ReadFile(filepath.Join(home, ".junie", "guidelines.md"))
	if err != nil {
		t.Fatalf("read guidelines.md: %v", err)
	}
	if !strings.Contains(string(guidelines), "<!-- servlo:begin -->") {
		t.Errorf("guidelines.md missing servlo block sentinel")
	}
	if !strings.Contains(string(guidelines), "<!-- servlo:end -->") {
		t.Errorf("guidelines.md missing servlo end sentinel")
	}
}

func TestWriteGlobalAISkills_idempotent(t *testing.T) {
	home := t.TempDir()

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("second call: %v", err)
	}

	guidelines, err := os.ReadFile(filepath.Join(home, ".junie", "guidelines.md"))
	if err != nil {
		t.Fatalf("read guidelines: %v", err)
	}
	if got := strings.Count(string(guidelines), "<!-- servlo:begin -->"); got != 1 {
		t.Errorf("expected 1 servlo:begin sentinel, got %d", got)
	}
	if got := strings.Count(string(guidelines), "<!-- servlo:end -->"); got != 1 {
		t.Errorf("expected 1 servlo:end sentinel, got %d", got)
	}
}

func TestWriteGlobalAISkills_preservesExistingGuidelines(t *testing.T) {
	home := t.TempDir()

	guidelinesPath := filepath.Join(home, ".junie", "guidelines.md")
	if err := os.MkdirAll(filepath.Dir(guidelinesPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "# Project guidelines\n\nFollow house style.\n"
	if err := os.WriteFile(guidelinesPath, []byte(existing), 0644); err != nil {
		t.Fatalf("seed guidelines: %v", err)
	}

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("WriteGlobalAISkills: %v", err)
	}

	got, err := os.ReadFile(guidelinesPath)
	if err != nil {
		t.Fatalf("read guidelines: %v", err)
	}
	if !strings.Contains(string(got), "Follow house style.") {
		t.Errorf("existing guidelines content was dropped")
	}
	if !strings.Contains(string(got), "<!-- servlo:begin -->") {
		t.Errorf("servlo block not appended")
	}
}

func TestMcpEnabledGlobally_noMarkers(t *testing.T) {
	home := t.TempDir()
	if mcpEnabledGlobally(home) {
		t.Errorf("expected false when no markers present")
	}
}

func TestMcpEnabledGlobally_detectsClaudeSkill(t *testing.T) {
	home := t.TempDir()
	skill := filepath.Join(home, ".claude", "skills", "servlo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skill, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !mcpEnabledGlobally(home) {
		t.Errorf("expected true when SKILL.md marker exists")
	}
}

func TestMcpEnabledGlobally_detectsCursorRules(t *testing.T) {
	home := t.TempDir()
	rules := filepath.Join(home, ".cursor", "rules", "servlo.mdc")
	if err := os.MkdirAll(filepath.Dir(rules), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(rules, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !mcpEnabledGlobally(home) {
		t.Errorf("expected true when servlo.mdc marker exists")
	}
}

func TestWriteGlobalAISkills_replacesExistingServloBlock(t *testing.T) {
	home := t.TempDir()

	guidelinesPath := filepath.Join(home, ".junie", "guidelines.md")
	if err := os.MkdirAll(filepath.Dir(guidelinesPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := "# guidelines\n\n<!-- servlo:begin -->\nstale servlo content\n<!-- servlo:end -->\n"
	if err := os.WriteFile(guidelinesPath, []byte(stale), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("WriteGlobalAISkills: %v", err)
	}

	got, err := os.ReadFile(guidelinesPath)
	if err != nil {
		t.Fatalf("read guidelines: %v", err)
	}
	if strings.Contains(string(got), "stale servlo content") {
		t.Errorf("stale servlo block was not replaced")
	}
	if !strings.Contains(string(got), "Servlo, a local PHP development environment") {
		t.Errorf("fresh servlo block not written")
	}
}

func TestProjectHasServloSkills(t *testing.T) {
	dir := t.TempDir()
	if ProjectHasServloSkills(dir) {
		t.Fatalf("empty dir should not be opted in")
	}

	skill := filepath.Join(dir, ".claude", "skills", "servlo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skill, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !ProjectHasServloSkills(dir) {
		t.Errorf("SKILL.md presence should signal opt-in")
	}

	dir2 := t.TempDir()
	guidelines := filepath.Join(dir2, ".junie", "guidelines.md")
	if err := os.MkdirAll(filepath.Dir(guidelines), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(guidelines, []byte("header only, no servlo markers\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ProjectHasServloSkills(dir2) {
		t.Errorf("guidelines without servlo marker should not signal opt-in")
	}

	if err := os.WriteFile(guidelines, []byte("junk\n<!-- servlo:begin -->\nstuff\n<!-- servlo:end -->\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !ProjectHasServloSkills(dir2) {
		t.Errorf("guidelines with servlo marker should signal opt-in")
	}
}

func TestWriteProjectAISkills_writesAllArtefacts(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("WriteProjectAISkills: %v", err)
	}

	want := []string{
		".mcp.json",
		".cursor/mcp.json",
		".junie/mcp/mcp.json",
		".gemini/settings.json",
		".vscode/mcp.json",
		".claude/skills/servlo/SKILL.md",
		".cursor/rules/servlo.mdc",
		".junie/guidelines.md",
		"GEMINI.md",
		"AGENTS.md",
		".github/copilot-instructions.md",
	}
	for _, rel := range want {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", rel)
		}
	}
	// Codex MCP is global-only: no project config file should be written.
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Errorf("expected no project .codex/config.toml (Codex is global-only), err=%v", err)
	}
	// Windsurf is global-only and .ai/ belongs to Laravel Boost: servlo must never
	// write a project .ai/mcp/mcp.json.
	if _, err := os.Stat(filepath.Join(dir, ".ai", "mcp", "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("expected no project .ai/mcp/mcp.json (Windsurf is global-only), err=%v", err)
	}
	if !ProjectHasServloSkills(dir) {
		t.Errorf("ProjectHasServloSkills should return true after WriteProjectAISkills")
	}
}

func TestWriteProjectAISkills_skipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("first call: %v", err)
	}

	skill := filepath.Join(dir, ".claude", "skills", "servlo", "SKILL.md")
	rules := filepath.Join(dir, ".cursor", "rules", "servlo.mdc")

	oldSkillMtime := mtimeOrFail(t, skill)
	oldRulesMtime := mtimeOrFail(t, rules)

	time.Sleep(10 * time.Millisecond)

	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if got := mtimeOrFail(t, skill); !got.Equal(oldSkillMtime) {
		t.Errorf("SKILL.md was rewritten despite unchanged content (mtime changed from %v to %v)", oldSkillMtime, got)
	}
	if got := mtimeOrFail(t, rules); !got.Equal(oldRulesMtime) {
		t.Errorf("servlo.mdc was rewritten despite unchanged content (mtime changed from %v to %v)", oldRulesMtime, got)
	}
}

func TestWriteProjectAISkills_rewritesWhenContentChanges(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, ".claude", "skills", "servlo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skill, []byte("stale content, older schema"), 0644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != renderClaudeSkill() {
		t.Errorf("stale SKILL.md was not refreshed")
	}
}

func mtimeOrFail(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}

func TestRemoveMCPServerEntry_missingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	changed, err := removeServerJSON(path, "mcpServers", "servlo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Errorf("missing file should not report changed=true")
	}
}

func TestRemoveMCPServerEntry_missingEntryIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	_ = os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0644)

	changed, err := removeServerJSON(path, "mcpServers", "servlo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Errorf("missing entry should not report changed=true")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"other"`) {
		t.Errorf("other entry was lost: %s", data)
	}
}

func TestRemoveMCPServerEntry_preservesOtherEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	_ = os.WriteFile(path, []byte(`{"mcpServers":{"servlo":{"command":"servlo"},"other":{"command":"x"}}}`), 0644)

	changed, err := removeServerJSON(path, "mcpServers", "servlo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"servlo"`) {
		t.Errorf("servlo entry should be gone: %s", data)
	}
	if !strings.Contains(string(data), `"other"`) {
		t.Errorf("other entry was dropped: %s", data)
	}
}

func TestRemoveMCPServerEntry_deletesFileWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	_ = os.WriteFile(path, []byte(`{"mcpServers":{"servlo":{"command":"servlo"}}}`), 0644)

	changed, err := removeServerJSON(path, "mcpServers", "servlo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be removed when empty, got err=%v", err)
	}
}

func TestStripJunieServloSection_removesDelimitedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guidelines.md")
	content := "# Project guidelines\n\nsomething custom\n\n<!-- servlo:begin -->\nservlo stuff\n<!-- servlo:end -->\n"
	_ = os.WriteFile(path, []byte(content), 0644)

	changed, err := stripSentinelSection(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "servlo:begin") || strings.Contains(string(got), "servlo stuff") {
		t.Errorf("servlo block should be gone:\n%s", got)
	}
	if !strings.Contains(string(got), "something custom") {
		t.Errorf("user content was lost:\n%s", got)
	}
}

func TestStripJunieServloSection_deletesFileWhenOnlyServloBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guidelines.md")
	content := "<!-- servlo:begin -->\nservlo stuff\n<!-- servlo:end -->\n"
	_ = os.WriteFile(path, []byte(content), 0644)

	changed, err := stripSentinelSection(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be removed when only servlo block present, got err=%v", err)
	}
}

func TestStripJunieServloSection_missingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guidelines.md")
	changed, err := stripSentinelSection(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Errorf("missing file should not report changed=true")
	}
}

func TestRemoveGlobalAISkills_roundTripWithWrite(t *testing.T) {
	home := t.TempDir()
	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := RemoveGlobalAISkills(home, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, rel := range []string{
		".claude/skills/servlo/SKILL.md",
		".cursor/rules/servlo.mdc",
		".junie/guidelines.md",
	} {
		if _, err := os.Stat(filepath.Join(home, rel)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed, err=%v", rel, err)
		}
	}
}

func TestRemoveProjectAISkills_roundTripWithWrite(t *testing.T) {
	abs := t.TempDir()
	if err := WriteProjectAISkills(abs, false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ProjectHasServloSkills(abs) == false {
		t.Fatal("precondition: write should have produced markers")
	}
	if err := RemoveProjectAISkills(abs, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ProjectHasServloSkills(abs) {
		t.Errorf("ProjectHasServloSkills should be false after remove")
	}
	for _, rel := range []string{
		".claude/skills/servlo/SKILL.md",
		".cursor/rules/servlo.mdc",
		".mcp.json",
		".cursor/mcp.json",
		".ai/mcp/mcp.json",
		".junie/mcp/mcp.json",
		".junie/guidelines.md",
		".gemini/settings.json",
		".vscode/mcp.json",
		"GEMINI.md",
		"AGENTS.md",
		".github/copilot-instructions.md",
	} {
		if _, err := os.Stat(filepath.Join(abs, rel)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed, err=%v", rel, err)
		}
	}
}

func TestRunMCPEject_roundTripWithInject(t *testing.T) {
	dir := t.TempDir()
	if err := runMCPInject(dir); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !ProjectHasServloSkills(dir) {
		t.Fatal("precondition: inject should have produced markers")
	}
	if err := runMCPEject(dir); err != nil {
		t.Fatalf("eject: %v", err)
	}
	if ProjectHasServloSkills(dir) {
		t.Errorf("ProjectHasServloSkills should be false after eject")
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf(".mcp.json should be gone after eject, err=%v", err)
	}
}

func TestRemoveProjectAISkills_preservesUnrelatedMCPEntries(t *testing.T) {
	abs := t.TempDir()
	_ = os.WriteFile(filepath.Join(abs, ".mcp.json"),
		[]byte(`{"mcpServers":{"servlo":{"command":"servlo"},"other":{"command":"x"}}}`), 0644)

	if err := RemoveProjectAISkills(abs, false); err != nil {
		t.Fatalf("remove: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(abs, ".mcp.json"))
	if err != nil {
		t.Fatalf("file should be preserved when other entries remain: %v", err)
	}
	if strings.Contains(string(data), `"servlo"`) {
		t.Errorf("servlo should be gone: %s", data)
	}
	if !strings.Contains(string(data), `"other"`) {
		t.Errorf("other should be preserved: %s", data)
	}
}

func TestIsServloBuiltImage_matchers(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"servlo-php84-fpm:local", true},
		{"servlo-php83-fpm:local", true},
		{"servlo-custom-my-app:local", true},
		{"servlo-dnsmasq:local", true},
		{"docker.io/library/mysql:8.0", false},
		{"docker.io/dunglas/frankenphp:php8.4-alpine", false},
		{"servlo-nginx:alpine", false},
		{"some-other:tag", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := isServloBuiltImage(tt.ref); got != tt.want {
				t.Errorf("isServloBuiltImage(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

// TestServloReference_underSizeCeiling guards against accidental re-bloat of the
// single canonical reference. It ships into every registered project and
// globally for every client, so drift upward gets expensive fast. Raise the
// ceiling only when adding content that justifies the bytes. Unifying the three
// former per-client constants onto this one leaner reference dropped the prior
// 57000-byte SKILL.md ceiling to 26000; bumped to this for the `workspace` tool
// group and for the package-manager, worker-state and preset-metadata rules an
// assistant was previously getting wrong, then 28500 → 28700 for the `diag`
// `doctor_fix` action, then 28700 → 29400 for the runtime `ini_*` php.ini
// actions and the shared-vs-per-version guidance, then 29400 → 29700 for the
// fnm/nvm version-manager choice (`node.manager`), then 29700 → 30300 for the
// db `import` provider-dump handling and the `php_list` base-image update flag,
// then 30300 → 30800 for the worktree `wait` action and the readiness rule it
// exists to replace: an assistant that guesses from the tree's contents races
// the watcher's installer, and no amount of probing files can tell it apart.
// The 30800 → 31000 bump is not new content: the S0.1 rename made every
// occurrence of the product name two bytes longer.
func TestServloReference_underSizeCeiling(t *testing.T) {
	const ceiling = 31000
	if got := len(servloReference); got > ceiling {
		t.Errorf("servlo-reference.md is %d bytes, ceiling is %d — trim before raising", got, ceiling)
	}
}
