package surfacescan

// specs are the documents that describe the deletions and so name every
// deleted feature on purpose, plus this package, whose whole content is the
// list of forbidden names.
var specs = []string{
	"CLAUDE.md", "PRD.md", "STORY.md", "CHANGELOG.md", "README.md",
	".claude/", "internal/surfacescan/",
}

// Rules is the deleted-feature surface. Enforced rules fail the gate; the rest
// name a feature whose story has not run yet and are reported as pending.
func Rules() []Rule {
	return []Rule{
		{
			Feature: "system tray", Story: "S0.2", Enforced: true,
			Patterns: []string{`(?i)\btray\b`, `\bsystray\b`, `getlantern`},
			Allow:    specs,
		},
		{
			Feature: "nogui build tag", Story: "S0.2", Enforced: true,
			Patterns: []string{`\bnogui\b`},
			Allow:    specs,
		},
		{
			Feature: "desktop notifications", Story: "S0.2", Enforced: true,
			Patterns: []string{`notify-send`, `\bbeeep\b`, `gen2brain`, `\bNotifySend\b`},
			Allow:    specs,
		},
		{
			Feature: "WSL2 code paths", Story: "S0.2", Enforced: true,
			Patterns: []string{`(?i)\bwsl2?\b`},
			Allow:    specs,
		},
		{
			Feature: "macOS and Windows build surface", Story: "S0.2", Enforced: true,
			Patterns: []string{
				`_darwin(_test)?\.go$`, `_windows(_test)?\.go$`,
				`^//go:build.*\b(darwin|windows)\b`,
				`"launchctl"`,
			},
			Allow: specs,
		},
		{
			Feature: "MCP server", Story: "S0.4", Enforced: true,
			Patterns: []string{`(?i)\bmcp\b`, `ModelContextProtocol`, `modelcontextprotocol`},
			Allow:    specs,
		},

		// Pending. Each is deleted by the story named, which turns its rule on.
		{
			Feature: "Tinker REPL", Story: "S0.5",
			Patterns: []string{`(?i)\btinker\b`, `phpantom_lsp`},
			Allow:    specs,
		},
		{
			Feature: "container shell drop-in", Story: "S0.5",
			Patterns: []string{`servlo shell\b`, `\bcmdShell\b`},
			Allow:    specs,
		},
		{
			Feature: "SPX profiler", Story: "S0.5",
			Patterns: []string{`(?i)\bspx\b`, `internal/profiler`},
			Allow:    specs,
		},
		{
			Feature: "dump()/dd() bridge", Story: "S0.5",
			Patterns: []string{`auto_prepend`, `servlo-dump`, `internal/dumps`, `servlo_devtools`},
			Allow:    specs,
		},
		{
			Feature: "Xdebug toggles", Story: "S0.5",
			Patterns: []string{`(?i)\bxdebug\b`},
			Allow:    specs,
		},
		{
			Feature: "browser php.ini editing", Story: "S0.5",
			Patterns: []string{`ini_write`, `handlePHPIniWrite`},
			Allow:    specs,
		},
		{
			Feature: "git worktrees", Story: "S0.6",
			Patterns: []string{`(?i)\bworktree\b`},
			Allow:    specs,
		},
		{
			Feature: "idle-suspend", Story: "S0.6",
			Patterns: []string{`idle-suspend`, `internal/idle`, `\bidleSuspend\b`},
			Allow:    specs,
		},
		{
			Feature: "LAN and tunnel sharing", Story: "S0.6",
			Patterns: []string{`lanshare`, `lan:expose`, `\btunnel\b`},
			Allow:    specs,
		},
		{
			Feature: ".test domains and host resolver mutation", Story: "S0.6",
			Patterns: []string{`\bdnsmasq\b`, `\.localhost\b`, `dns:repair`, `\bsudoers\b`},
			Allow:    specs,
		},
		{
			Feature: "mkcert", Story: "S0.6",
			Patterns: []string{`\bmkcert\b`},
			Allow:    specs,
		},
		{
			Feature: "Mailpit", Story: "S0.6",
			Patterns: []string{`(?i)\bmailpit\b`},
			Allow:    specs,
		},
		{
			Feature: "inline service definitions", Story: "S0.7",
			Patterns: []string{`InlineService`, `custom_containers`},
			Allow:    specs,
		},
		{
			Feature: "launchd and Homebrew residue in comments", Story: "S0.5",
			Patterns: []string{`(?i)\blaunchd\b`, `(?i)\bhomebrew\b`, `Library/Logs`},
			Allow:    specs,
		},
	}
}
