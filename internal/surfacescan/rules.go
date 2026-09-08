package surfacescan

// specs are the documents that describe the deletions and so name every
// deleted feature on purpose, plus this package, whose whole content is the
// list of forbidden names.
var specs = []string{
	"CLAUDE.md", "PRD.md", "STORY.md", "CHANGELOG.md", "README.md",
	"SECURITY.md", ".claude/", "internal/surfacescan/",
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
	"internal/nginx/panel_vhost.go",
	"internal/nginx/trust_token.go",
	"internal/ui/dashproxy.go",
	"internal/ui/dashproxy_test.go",
	"internal/ui/local_control_test.go",
	"internal/ui/panel_public_test.go",
	"internal/ui/remote_control_test.go",
	"internal/ui/site_env_test.go",
	"internal/ui/ws_origin_test.go",
	"internal/ui/web/src/lib/api.ts",
	"internal/ui/server.go",
	"internal/ui/remote_control.go",
	"internal/ui/wsframe.go",
	"internal/ui/wsframe_test.go",
	"docs/features/index.md",
	"docs/features/panel-access.md",
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
			// The HTTP surface is named as well as the Go plumbing. The original
			// patterns described how the bridge was built, so the routes it was
			// reached through survived the deletion: the docs demo went on
			// stubbing /api/dumps and /api/devtools long after nothing served
			// them, and the build only broke when a fixture was removed.
			Patterns: []string{
				`auto_prepend`, `servlo-dump`, `internal/dumps`, `servlo_devtools`,
				`/api/dumps`, `/api/devtools`,
				// The message keys too. Deleting a feature's Go and its routes
				// left fifty-one locale strings for its Debug tab sitting in
				// every one of the fourteen message files, translated, for three
				// phases. `debug_` on its own is too ordinary a prefix to
				// forbid, so this names the tab's own keys rather than the word.
				`\bdumps_[a-z]`, `\bnav_dumps\b`, `dumpBridge`,
				`\bdebug_(tab|disabled|waiting|loadMore|show_tests|tests_hidden)`,
			},
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
			// lan:expose was carved out of this rule for a phase and a half on
			// the grounds that a server needs to choose its bind. It does not:
			// nginx serves the sites, so it publishes on every interface and
			// everything else stays on loopback, and neither half is a setting.
			// What the toggle actually did was default a fresh production
			// install to loopback, where it served nobody.
			Patterns: []string{`(?i)lanshare`, `lan:share`, `lan_share`, `lan:expose`, `lan_expose`, `LANExposed`, `BindForLAN`, `tunnelshare`, `tunnel_url`, `Tunnel(Start|Stop|Status)`, `servlo share`, `\bngrok\b`, `cloudflared`},
			Allow: append(append([]string{}, specs...),
				"internal/hostbin/hostbin_test.go",
			),
		},
		// The DNS stack and the local CA outlived Phase 0 on purpose: they are
		// what Phase 1 replaces with real domains and real certificates, so they
		// came out as those stories landed rather than leaving the tree unable
		// to serve anything in between. Mailpit came out the same way in S13.0,
		// once per-site SMTP gave a site somewhere for its mail to go.
		{
			// Split from the rule below, and the split is the point.
			//
			// Allow exempts a file from a whole rule, not from one pattern. The
			// panel's own hostname is servlo.localhost, so install.go sat on the
			// allowlist for `.localhost` and was thereby exempt from `dnsmasq`
			// too. It kept a prompt offering to manage DNS for local sites, and
			// the gate that exists to catch exactly that said nothing for a
			// phase and a half. One allowlist per thing being excused.
			Feature: ".localhost site mode", Story: "S2.1", Enforced: true,
			Patterns: []string{`\.localhost\b`},
			// servlo.localhost is not the deleted .localhost site mode: RFC 6761
			// makes it resolve to loopback with no DNS at all, which is why the
			// dashboard uses it.
			Allow: append(append(append([]string{}, specs...), panelVhostFiles...),
				"install.sh", "tests/installer/installer.bats",
			),
		},
		{
			Feature: ".test domains and host resolver mutation", Story: "S2.1", Enforced: true,
			// The resolver paths are the story's own acceptance criterion: a test
			// asserting no host resolver file is ever written. Naming the paths
			// catches a rewrite that reaches for them under any other name.
			Patterns: []string{
				`\bdnsmasq\b`, `dns:repair`, `\bsudoers\b`,
				`/etc/resolv\.conf`, `NetworkManager/conf\.d`, `resolved\.conf\.d`, `/etc/resolver`,
				`\bresolvectl\b`, `\bsystemd-resolved\b`,
			},
			// install.sh and its tests keep the teardown for the root-owned files
			// an older servlo wrote, including a passwordless sudoers grant. They
			// name those paths to remove them, never to create them, and dropping
			// the teardown would strand that grant on every upgraded machine.
			Allow: append(append([]string{}, specs...),
				"install.sh", "tests/installer/installer.bats",
			),
		},
		{
			// Split from the rule above for the reason the .localhost split
			// documents: Allow exempts a file from a whole rule. The fixture
			// domains that fill every _test.go are harmless, but tolerating
			// them in the rule above would tolerate a dnsmasq container coming
			// back in one too.
			//
			// The rule above named the resolver files and the DNS container and
			// never the TLD itself, so `.test` was the one thing the ".test
			// domains" rule did not look for. What that cost: `servlo domain
			// add`'s own help still told the operator to type a name "without
			// .test", and `servlo env` still said it set APP_URL to "the
			// registered .test domain", a phase and a half after the stack that
			// resolved such a name was deleted.
			Feature: ".test domains in shipped text", Story: "S2.1", Enforced: true,
			// Two shapes, because the residue took two. A domain-shaped .test
			// (myapp.test), with the character that follows it constrained so a
			// JavaScript `re.test(s)` call and a Vitest .test.ts path are not
			// mistaken for domains; and a bare `.test` standing as a word in
			// prose, which is how the help strings and the stale comments named
			// the TLD. A quoted ".test" is deliberately matched by neither: it
			// is a suffix check, and the one that matters is allowed below.
			Patterns: []string{`[A-Za-z0-9*-]+\.test([^(A-Za-z0-9_.]|$)`, `\s\.test\b`},
			Allow: append(append([]string{}, specs...),
				// Fixture domains. .test is the RFC 6761 TLD reserved for
				// exactly this, so a test that needs a domain should use one;
				// what was deleted is servlo resolving it, not the reservation.
				"_test.go", ".test.ts",
				// The teardown for the root-owned files an older servlo wrote.
				"install.sh", "tests/installer/installer.bats",
				// Names the deleted stack to explain its own relationship to
				// it: what container DNS was never part of.
				"internal/podman/containerdns.go",
				// The WebSocket DNS-rebinding defence accepts a hostname only
				// under a reserved local TLD. .test is one whether or not servlo
				// resolves it, and a site served on such a name still needs its
				// own same-origin socket to open.
				"internal/ui/wsframe.go",
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
			// Three buttons that all did the same thing: spawn a desktop
			// application on the machine running servlo. A terminal emulator
			// tailing a unit, a file manager on a site's directory, an IDE on a
			// file and line. A headless droplet has no desktop for any of them to
			// appear on, so each spawned a process nobody would ever see while
			// being an execution surface on the box that runs every site.
			//
			// Not the same thing as the container shell drop-in S0.5 deleted,
			// which is why the terminal one outlived it: that entered a
			// container, this opened a window.
			Feature: "host desktop launchers", Story: "S5.7", Enforced: true,
			Patterns: []string{
				`openTerminal`, `update-terminal`, `logs/terminal`,
				`x-terminal-emulator`, `gnome-terminal`, `konsole`,
				`open-folder`, `open-editor`, `openInEditor`, `editorCommand`,
			},
			Allow: specs,
		},
		{
			Feature: "Mailpit", Story: "S13.0", Enforced: true,
			// mailhog is named too: the Sail importer used to translate it into
			// the mailpit preset, so leaving the old name behind would let the
			// catcher back in under the alias it arrived by.
			Patterns: []string{`(?i)\bmailpit\b`, `(?i)\bmailhog\b`},
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
			// The rename that S0.1 performed swept the repository, and this is
			// what keeps it swept. It was scoped to stores/ for a while, on the
			// grounds that a repository-wide rule would need an allowlist that
			// grew every time the image-ref files were touched. That allowlist
			// is eleven entries and has not moved since, which is a cheaper
			// price than the alternative: with the narrow rule, a stale
			// reference could sit in a comment, a skill or a doc for a phase
			// and nothing would say so, and three of them did.
			//
			// The regression it was written for was not cosmetic. Container
			// hostnames like lerd-redis pointed at containers servlo never
			// creates, so every service integration in the copied definitions
			// was broken, and every database preset shipped the same published
			// password.
			Feature: "upstream project name", Story: "S0.1", Enforced: true,
			// Case-insensitive: the first version of this rule was not, and
			// LERD_POSTGRES_HOSTS survived it in the pgadmin definition, where
			// it quietly broke that preset's family discovery.
			Patterns: []string{`(?i)\blerd\b`},
			// One thing is excused: the documents that describe the fork, which
			// carry its single permitted statement of it in README.md. The
			// prebuilt PHP base images were the other exemption and are not
			// any more, now that they publish under Servlo's own namespace.
			Allow: specs,
		},
		{
			Feature: "launchd and Homebrew residue", Story: "S0.2", Enforced: true,
			Patterns: []string{`(?i)\blaunchd\b`, `(?i)\bhomebrew\b`, `Library/Logs`},
			Allow:    specs,
		},
		{
			// A browser launcher is a laptop's idea of a command. Servlo runs on
			// a headless server reached over ssh, where xdg-open has nothing to
			// open and `.Start()` does not wait to find out, so the whole
			// feature was a command that silently did nothing.
			Feature: "browser launchers", Story: "S0.6", Enforced: true,
			// xdg-open is the mechanism, so it is the pattern that matters.
			// A bare openBrowser is deliberately not forbidden: it is the
			// natural name for a click handler in the dashboard's own
			// JavaScript, which opens a link in the visitor's browser and is
			// nothing to do with launching one on the server.
			Patterns: []string{`xdg-open`, `\bbrowserOpener\b`, `skip-open`},
			Allow:    specs,
		},
		{
			// stripe listen --forward-to relays webhooks to an endpoint Stripe
			// cannot reach, which is a laptop. A site here is on a real domain
			// with a real certificate, so Stripe delivers to it directly and the
			// forwarder is a long-lived process holding a live key for nothing.
			// The key was also interpolated into the unit's ExecStart, putting a
			// live secret in the process argument list, where every site on the
			// machine could read it out of ps: they all run as the same user.
			Feature: "Stripe webhook listener and mock", Story: "S0.6", Enforced: true,
			// The unit name and the worker name, not just the command.
			//
			// The first pass at this rule matched the listener's own vocabulary
			// — stripe listen, stripe-mock, StripeListener — and deleted the two
			// files that spoke it. Everything built around them answered to a
			// different name and survived: StripeWorkerName, registeredStripeUnits,
			// servlo-stripe-<site> in the status output, the bug report, the
			// autostart set, and a "stripe:refresh" post to a daemon endpoint
			// that no longer existed, fired on every certificate issuance.
			//
			// A feature is not the code that implements it. It is also every
			// name the rest of the tree knows it by, which is what these match.
			Patterns: []string{
				`(?i)stripe[-_:]?listen`, `stripe-mock`, `stripe-cli`, `StripeListener`,
				`servlo-stripe`, `StripeWorkerName`, `[Ss]tripeUnits`, `stripe:refresh`,
			},
			// The demo's fixture logs carry a PHP exception from a site that
			// uses Stripe's own SDK. An application billing its customers is not
			// this feature; servlo running a webhook forwarder for it was.
			Allow: append(append([]string{}, specs...),
				"internal/ui/web/demo/fixtures/cron.json",
				"internal/ui/web/demo/stubs.ts",
			),
		},
		{
			// The dnsmasq container the S2.1 rule forbids was packaged as the
			// servlo-dns quadlet, and that name is not dnsmasq, so nothing
			// caught what it left: a PublishPort special case preserving :5300
			// for a quadlet that no longer ships, two reserved-name lists that
			// skipped "dns" and "dns-forwarder" (and so would have hidden a
			// leftover unit from the orphan scan that exists to report it), a
			// port probe in the bug report, and a comment about an in-process
			// DNS watcher that had already been deleted.
			Feature: "the servlo-dns container", Story: "S2.1", Enforced: true,
			Patterns: []string{`servlo-dns`, `servlo0\b`, `dns-forwarder`},
			// install.sh removes the root-owned link unit an older servlo wrote,
			// and names it to do so.
			Allow: append(append([]string{}, specs...),
				"install.sh", "tests/installer/installer.bats",
			),
		},
		{
			// Browser testing is a thing CI does, not a thing a server does.
			// This baked chromium into the shared PHP-FPM image, the one every
			// site on the machine is served from.
			Feature: "in-container browser testing", Story: "S0.6", Enforced: true,
			// PLAYWRIGHT_BROWSERS_PATH rather than the word playwright: the
			// mechanism was baking a browser into the shared image and pointing
			// the registry at it. A local script that renders brand assets with
			// playwright on somebody's own machine is not this, and forbidding
			// the bare word would only teach the next person to allowlist a
			// directory out of the whole rule.
			// Selenium is not matched by name here. It is a plausible fixture
			// name in a test about services in general, and forbidding the word
			// would mean allowlisting half the podman and serviceops tests out
			// of this rule entirely — which is how S2.1's leftover survived.
			// What actually matters is that no preset the binary ships is a
			// testing tool, and TestNoTestingPresetsShip asserts exactly that
			// against the store index rather than against prose.
			Patterns: []string{`pest[-_:]?browser`, `PLAYWRIGHT_BROWSERS_PATH`},
			Allow:    specs,
		},
		{
			// The worker-mode setting existed to choose between exec and
			// container workers on macOS, and its own help said Linux ignores
			// it. S0.2 deleted the macOS paths; this is what they left behind.
			Feature: "macOS worker runtime mode", Story: "S0.2", Enforced: true,
			// WorkerExecMode is the same setting under the name it kept: a
			// choice between exec and container workers, ignored on Linux,
			// which is the only platform there is. The three patterns above
			// missed it, and the TUI still had a switch wired to a `workers
			// mode` command that had been deleted.
			Patterns: []string{`\bworkersMode\b`, `\bWorkerMigrationActive\b`, `workers mode`,
				`WorkerExecMode`, `\bexec_mode\b`, `sysWorkerMode`},
			Allow: specs,
		},
		{
			// The header vouched for requests that reached the panel over the
			// podman bridge instead of a unix socket, which is what the vhost
			// did on macOS. S0.2 deleted the macOS paths and nothing servlo
			// writes has injected it since, so all it could still do was make a
			// request from anywhere count as local on a panel that listens on
			// 0.0.0.0.
			Feature: "the nginx trust-token bypass", Story: "S0.2", Enforced: true,
			Patterns: []string{`X-Servlo-Trust`, `TrustToken`, `nginx-trust-token`},
			// The regression test has to name the header it proves buys
			// nothing, and it is the only thing excused.
			Allow: append(append([]string{}, specs...), "internal/ui/local_control_test.go"),
		},
	}
}
