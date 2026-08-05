package surfacescan

// specs are the documents that describe the deletions and so name every
// deleted feature on purpose, plus this package, whose whole content is the
// list of forbidden names.
var specs = []string{
	"CLAUDE.md", "PRD.md", "STORY.md", "CHANGELOG.md", "README.md",
	".claude/", "internal/surfacescan/",
}

// panelVhostFiles carry servlo.localhost, the panel's own hostname. That is not
// the deleted .localhost site mode: RFC 6761 makes .localhost resolve to
// loopback with no DNS at all, which is exactly why the dashboard uses it and
// why deleting the DNS stack does not touch it. Where the panel lives on a real
// server is Phase 2's question.
var panelVhostFiles = []string{
	"internal/cli/dashboard.go",
	"internal/cli/install.go",
	"internal/cli/pause.go",
	"internal/cli/startstop.go",
	"internal/config/paths.go",
	"internal/nginx/manager.go",
	"internal/nginx/manager_test.go",
	"internal/nginx/trust_token.go",
	"internal/ui/dashproxy.go",
	"internal/ui/dashproxy_test.go",
	"internal/ui/local_control_test.go",
	"internal/ui/remote_control_test.go",
	"internal/ui/site_env_test.go",
	"internal/ui/ws_origin_test.go",
	"internal/ui/web/src/lib/api.ts",
	"internal/ui/server.go",
	"internal/ui/remote_control.go",
	"internal/ui/wsframe.go",
	"internal/ui/wsframe_test.go",
	"docs/features/index.md",
	"docs/public/share.html",
	"docs/features/web-ui.md",
	"docs/reference/directory-layout.md",
	"docs/.vitepress/theme/components/LandingPage.vue",
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
		{
			Feature: "Tinker REPL", Story: "S0.5", Enforced: true,
			Patterns: []string{`(?i)\btinker\b`, `phpantom_lsp`},
			Allow:    specs,
		},
		{
			Feature: "container shell drop-in", Story: "S0.5", Enforced: true,
			Patterns: []string{`servlo shell\b`, `\bcmdShell\b`},
			Allow:    specs,
		},
		{
			Feature: "SPX profiler", Story: "S0.5", Enforced: true,
			Patterns: []string{`(?i)\bspx\b`, `internal/profiler`},
			Allow:    specs,
		},
		{
			Feature: "dump()/dd() bridge", Story: "S0.5", Enforced: true,
			Patterns: []string{`auto_prepend`, `servlo-dump`, `internal/dumps`, `servlo_devtools`},
			// The guards that keep auto_prepend_file out of a framework's php.ini,
			// and the test that proves no generated unit mounts the bridge, have to
			// name the thing they forbid.
			Allow: append(append([]string{}, specs...),
				"internal/config/framework.go",
				"internal/config/framework_phpini_test.go",
				"internal/cli/php_ini_test.go",
				"internal/podman/no_dump_bridge_test.go",
				"docs/usage/framework-definitions.md",
			),
		},
		{
			Feature: "Xdebug toggles", Story: "S0.5", Enforced: true,
			Patterns: []string{`(?i)\bxdebug\b`},
			Allow:    specs,
		},
		{
			Feature: "browser php.ini editing", Story: "S0.5", Enforced: true,
			Patterns: []string{`ini_write`, `handlePHPIniWrite`},
			Allow:    specs,
		},
		{
			Feature: "git worktrees", Story: "S0.6", Enforced: true,
			Patterns: []string{`(?i)\bworktree\b`},
			Allow:    specs,
		},
		{
			Feature: "idle-suspend", Story: "S0.6", Enforced: true,
			Patterns: []string{`idle-suspend`, `idle_suspend`, `internal/idle`, `\bidleSuspend\b`, `activityping`},
			Allow:    specs,
		},
		{
			Feature: "LAN and tunnel sharing", Story: "S0.6", Enforced: true,
			// lan:expose is deliberately absent: it decides whether nginx binds
			// loopback or every interface, which a server needs, and S1.2 owns it.
			Patterns: []string{`(?i)lanshare`, `lan:share`, `lan_share`, `tunnelshare`, `tunnel_url`, `Tunnel(Start|Stop|Status)`, `servlo share`, `\bngrok\b`, `cloudflared`},
			Allow: append(append([]string{}, specs...),
				"internal/hostbin/hostbin_test.go",
			),
		},
		// The DNS stack and the local CA outlived Phase 0 on purpose: they are
		// what Phase 1 replaces with real domains and real certificates, so they
		// came out as those stories landed rather than leaving the tree unable
		// to serve anything in between. Mailpit below is still pending, waiting
		// on the per-site SMTP settings that replace it.
		{
			Feature: ".test domains and host resolver mutation", Story: "S2.1", Enforced: true,
			// The resolver paths are the story's own acceptance criterion: a test
			// asserting no host resolver file is ever written. Naming the paths
			// catches a rewrite that reaches for them under any other name.
			Patterns: []string{
				`\bdnsmasq\b`, `\.localhost\b`, `dns:repair`, `\bsudoers\b`,
				`/etc/resolv\.conf`, `NetworkManager/conf\.d`, `resolved\.conf\.d`, `/etc/resolver`,
				`\bresolvectl\b`, `\bsystemd-resolved\b`,
			},
			// The panel's own vhost is servlo.localhost, which is not the deleted
			// .localhost site mode: RFC 6761 makes it resolve to loopback with no
			// DNS at all, which is why the dashboard uses it. Where the panel
			// lives on a real server is Phase 2's question, not this story's.
			// install.sh and its tests keep the teardown for the root-owned files
			// an older servlo wrote, including a passwordless sudoers grant. They
			// name those paths to remove them, never to create them, and dropping
			// the teardown would strand that grant on every upgraded machine.
			Allow: append(append(append([]string{}, specs...), panelVhostFiles...),
				"install.sh", "tests/installer/installer.bats",
			),
		},
		{
			Feature: "mkcert", Story: "S3.1", Enforced: true,
			// The names of the trust plumbing go too, not just the binary: a
			// certificate issued by a CA only this machine trusts is the wrong
			// shape for a real domain however it is installed, so the NSS
			// databases and the system anchor have no successor to come back for.
			Patterns: []string{`\bmkcert\b`, `\bcertutil\b`, `\bnss(db|_tools|-tools)\b`, `rootCA\.pem`, `\bCAROOT\b`},
			Allow:    specs,
		},
		{
			Feature: "Mailpit", Story: "S13.0",
			Patterns: []string{`(?i)\bmailpit\b`},
			Allow:    specs,
		},
		{
			Feature: "inline service definitions", Story: "S0.7", Enforced: true,
			// A name tripwire, not the real guard: what actually holds is
			// ProjectService.Resolve returning nothing for an inline entry and
			// linkApplyServices refusing it, both pinned by their own tests.
			// This catches the shape coming back by its old names.
			Patterns: []string{`InlineService`, `custom_containers`, `SaveCustomService\(svc\.Custom`},
			Allow:    specs,
		},
		{
			Feature: "launchd and Homebrew residue", Story: "S0.2", Enforced: true,
			Patterns: []string{`(?i)\blaunchd\b`, `(?i)\bhomebrew\b`, `Library/Logs`},
			Allow:    specs,
		},
	}
}
