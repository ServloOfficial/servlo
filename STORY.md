# STORY.md — Servlo Epics & User Stories

Companion to `PRD.md` v1.1. Sizes: **S** ≤ 1 day, **M** ≤ 3 days, **L** ≤ 1 week, **XL** > 1 week.

**Standing rules, inherited from the upstream codebase and kept:**

- Write the failing test first. A change without test coverage does not merge.
- Behaviour belongs in store YAML, not Go. If you are branching on a framework name in Go, the logic is in the wrong layer.
- Environment variables belong to sites, never to workers.
- Run the full local gate before every commit: build, `go test ./...`, `go vet ./...`, `gofmt -l` empty, UI tests if the UI changed, installer bats tests if the installer changed.
- The droplet smoke test is deferred to the end of the build, once every phase has landed. It is not a gate on an individual story.

**Standing CI gate — the surface scan.** Enumerates the built binary's symbols and every API route. Fails the build if any deleted development feature reappears, or if any state-changing route lacks a declared permission. This runs from Phase 1 onward and never goes away.

---

# PHASE 0 — Fork and strip
*Goal: a clean, minimal, Ubuntu-only Servlo binary with nothing dangerous left in it.*

**S0.1 — Rename to Servlo.** ✅
Binary `servlo`; systemd unit prefix `servlo-` (`servlo-nginx`, `servlo-php84-fpm`, `servlo-panel`, `servlo-watcher`, `servlo-queue-<site>`); config at `~/.config/servlo/`; data at `~/.local/share/servlo/`; the Go module path becomes `github.com/realrashid/servlo`; the stale root `mkdocs.yml` is deleted, the docs site is VitePress under `docs/`.
*Done when:* no "lerd" string appears in user-facing output, unit names, paths or docs; the upstream MIT copyright notice is retained in `LICENSE`; the README states the fork relationship and links upstream. **M**

**S0.2 — Server-only build.** ✅
*Done when:* `make build-server` produces a CGO-free `servlo` binary; `cmd/lerd-tray` is **deleted outright**, along with desktop notifications and every macOS and WSL2 code path, and the `nogui` build tag goes with them; amd64 and arm64 both build.
Deleted, not build-tagged. A tray excluded by a tag is still in the tree and still one tag away from shipping, which is exactly what CLAUDE.md 3.1 forbids and what the surface scan asserts against. **S**

**S0.3 — Ubuntu-only platform gate.** ✅
*Done when:* distro detection collapses to Ubuntu; the installer refuses other distributions, and refuses Ubuntu 22.04 when Podman is below 4.5 while printing the upgrade path rather than half-installing. **M**

**S0.4 — Delete the MCP server.** ✅
*Done when:* the MCP package, all `mcp:*` commands and MCP documentation are removed; the surface scan asserts no MCP symbols remain. **M**

**S0.5 — Delete the code-execution surfaces.** ✅
Tinker REPL, container shell, SPX profiler, dump bridge, Xdebug toggles, browser `php.ini` editing.
*Done when:* all are removed from Go and the Svelte UI, **including the dump bridge's `auto_prepend` mount inside every generated PHP-FPM unit** — verified by inspecting a generated unit file, not by checking a runtime flag. **L**

**S0.6 — Delete local-development site features.** ✅
*Done when:* git worktrees, idle-suspend, LAN sharing and tunnel sharing are gone. **L**

**S0.7 — Reject inline service definitions from the project config file.** ✅
*Done when:* a project declaring an inline service is linked without it and the operator is told why; only reviewed store presets can run a container. **S**

**S0.8 — Bring the stores in-repo, and solve serving them.** ✅
*Done when:* `stores/frameworks/`, `stores/services/` and `stores/apps/` exist in this repository, mirroring upstream's subdir layout so `internal/origin/origin.go` needs only new base URLs; versions are pinned in config; definitions are verified before use; the promotion process is documented. **L**
The serving question is settled by embedding: `stores/stores.go` compiles the whole tree into the binary, so a fresh install never depends on the repository being reachable or public, and the fetch is the update path rather than the only way in.
Verification is by sha256 digests recorded in each index, not by signature. A signature is worth what the key ceremony behind it is, and a key committed to this repository would sign nothing; digests catch a definition changed independently of the index, which is the case a mirror or a `SERVLO_STORE_BASE_URL` override creates, and `verifyDigest` is the seam a signature check would slot into if the project ever holds a key.

**S0.9 — CI on Ubuntu 24.04.** ✅
*Done when:* build, unit tests, installer tests and the surface scan all run on a real 24.04 VM rather than a container. **M**

---

# PHASE 1 — Real server, real domains, real certificates
*Goal: a site on a real domain with a real certificate, served by Servlo. This milestone proves the whole thesis.*

## E1 — Server foundation

**S1.1 — Installer preflight.** ✅
*Done when:* Ubuntu version, Podman ≥ 4.5, crun, cgroup v2 and linger are all verified; anything unmet refuses cleanly with a specific message rather than half-installing. **M**

**S1.2 — Port binding.** ✅
*Done when:* the installer applies `net.ipv4.ip_unprivileged_port_start=0`, falls back to nftables DNAT 80→8080 / 443→8443 persisted across reboot, records the choice in config, and prints the sudo command rather than running it. **M**

**S1.3 — Automatic server basics.** ✅
*Done when:* a swap file sized to the droplet, the system timezone, unattended security updates and fail2ban for SSH are all configured at install. **SSH password authentication is left enabled and is never disabled by Servlo.** **L**

The SSH clause is enforced by a test rather than a comment: no basic may mention `sshd_config`, `PasswordAuthentication`, `PermitRootLogin` or restart sshd. That is the clause most likely to be violated by a well-meaning later change, so it is the one that gets a guard.

Swap is doubled below 2GB, matches RAM to 8GB and caps there. A machine that already swaps is left alone, and one whose memory cannot be read gets nothing rather than a guess. Security updates only, never an automatic reboot.

Commands are stored without sudo and gain it only when printed, so what bootstrap runs as root is exactly the operation an operator was shown. The same Plan/Satisfied/Commands shape as `internal/ports`, so every privileged step is reported and repaired the same way.

**S1.4 — Health verification of the port strategy.** ✅
*Done when:* `servlo doctor` confirms the strategy still holds on every run and specifically after a reboot. **S**

## E2 — Real domains

**S2.1 — Remove the `.test` DNS stack.** ✅
*Done when:* the dnsmasq container, its config, the host resolver mutation, the sudoers rule and `.localhost` mode are all gone; a test asserts no host resolver file is ever written. **L**

Reopened once, in the session that finished E6. The installer still carried a `--dns` flag, a prompt offering to "manage DNS for local sites (No: use *.localhost, no dnsmasq, no HTTPS)", and the three helpers behind it, none of them reachable from anything.

The surface scan should have caught that on the day it was written, and the reason it did not is worth more than the leftover code. `Allow` exempts a file from a whole *rule*, not from one *pattern*. The panel's own hostname is `servlo.localhost`, so `internal/cli/install.go` sat on the allowlist for `\.localhost` and was thereby exempt from `\bdnsmasq\b` as well. A gate that exists to catch exactly this said nothing for a phase and a half. The rule is two rules now, one per thing being excused, and the narrow one catches install.go the moment `dnsmasq` reappears there.

**S2.2 — Sites registered on real FQDNs.** ✅
*Done when:* site creation takes a full domain; the registry, vhost, `APP_URL` and certificate SANs all use it; no TLD is ever appended. **M**

## E3 — Certificates

**S3.1 — Extract a certificate-issuer interface.** ✅
*Done when:* mkcert is removed and the interface is in place; the inherited 30-day reissue scanner, expiry warnings and nginx reload all operate through it without modification. **M**

Two things landed alongside, both of which existed only to serve the local CA. The trust plumbing went with the binary (NSS databases, the system anchor, the certutil probes, the libnss3-tools prerequisite, the bootstrap trust flags) because a locally trusted certificate is the wrong shape for a real domain however it is installed. And the `dns.enabled` gate that refused HTTPS whenever servlo was not managing local DNS was gating on a deleted subsystem; whether a domain can be issued for is a live DNS question, which S3.3 answers, so the wizard keeps the plumbing and sources it from a predicate that says as much.

Between here and S3.2 the issuer in force refuses. It does not fall back to a self-signed certificate: on a real domain a browser cannot tell one apart from an interception, so shipping one would train the operator to click through the warning that protects them.

**S3.2 — HTTP-01 issuance from Let's Encrypt.** ✅
*Done when:* the challenge is served by the existing nginx container; the key is written 0600; a staging flag targets the ACME staging endpoint; the certificate installs and the vhost regenerates. **L**

The client is `x/crypto/acme`, which was already in the module graph, so this added no dependency. The challenge location reaches the templates as a method rather than a field, so a new vhost generator cannot forget it, and on a secured site it sits ahead of the HTTPS redirect.

The staging flag records the authority for the whole install rather than overriding one run, because issuance on staging and renewal on production would silently replace the tested certificate at the 30-day mark and spend the production rate limit the staging run existed to protect.

Still to come in this epic: the live DNS check that gates the button (S3.3), which is also what turns `httpsOfferable()` from a constant into a real answer.

**S3.3 — The Get SSL button.** ✅
*Done when:* the site panel shows a live DNS check resolving A and AAAA for the primary domain **and every alias**, compared against the droplet's public addresses. While unmatched the button is disabled and displays the actual mismatch — *"Waiting for DNS — example.com currently resolves to 1.2.3.4, this server is 5.6.7.8"*. Once matched, one click issues a certificate covering all domains, installs it, rewrites the vhost to 443, adds HSTS and a 301 redirect, and updates `APP_URL`. Progress and any ACME error stream into the panel. **L**

The gate lives in the certificate layer rather than the panel, so `servlo secure` refuses on the same terms and a wrong record cannot spend the rate limit from either direction. Whether it applies is the issuer's answer: HTTP-01 needs the authority to reach this server, DNS-01 will not, and S3.4 answers false.

Two calls worth recording. Every resolved record has to point here, not just one, because a stale address alongside the new one makes validation a coin flip and a half-failing renewal is harder to diagnose than one that never runs. And HSTS ships without `includeSubDomains` or `preload`, since the first breaks a group secondary deliberately left on plain http and the second is irreversible.

Progress is written to a per-domain file rather than streamed over the websocket: issuance is one synchronous request, so the panel polls the file while its own POST is in flight. Replacing that with a real event stream is worth doing when there is a second long-running action that needs one.

`httpsOfferable()` in the init wizard is still a constant. The wizard asks before a domain exists, so there is nothing to resolve at that point; the gate that matters runs at issuance.

**S3.4 — DNS-01 with Cloudflare, Route53 and DigitalOcean.** ✅
*Done when:* wildcards issue successfully; provider credentials are stored 0600 outside any site directory; covered by a staging integration test. **L**

No new dependency. Route53 is signed with SigV4 by hand rather than through the AWS SDK, which would bring dozens of modules for two API calls; it is also why the provider interface exists, since a signed API cannot share a code path with two bearer-token ones.

The wildcard subtlety worth remembering: `*.example.com` and `example.com` are separate authorizations sharing one record name, each with its own value, so both must be present at once. Publishing the second as a replacement withdraws the proof of the first.

The integration test runs against an in-process authority rather than Let's Encrypt staging. A staging run needs a real domain and a real registrar credential, which CI has neither of; the in-process authority validates against the records the provider actually holds and requires as many distinct values at the shared record as there are authorizations pointing at it, which is the property a staging run would be checking. The first version of that assertion counted non-empty values and passed with a broken record name, so it is worth keeping the stricter form.

Storing a credential is a separate command from using it: an operator may hold a token for one wildcard site and leave everything else on http-01, where none is needed.

**S3.5 — Renewal failure is loud.** ✅
*Done when:* a failed renewal produces a dashboard banner and an audit entry (and an email once panel SMTP exists). A site never silently serves an expired certificate. **M**

This pulled a minimal `internal/auditlog` forward from S5.6: append-only, one JSON entry per line, 0600, secrets redacted on the way in. S5.6 widens it to every state-changing route and adds rotation and the permission registry around it; nothing here should need to change for that, only grow.

Failures are recorded at the single issuance chokepoint rather than by each caller, so no route into issuance can forget. The record keeps the first failure time rather than the last, because "failing for three weeks" and "failed once this morning" are different problems and resetting the clock hides which one it is.

The expiry check is deliberately separate from the failure record: a machine restored from a backup has no failure records and can still be serving something expired, so what is on disk is checked on its own.

The email half waited on panel SMTP, and landed with it in S13.2.

**S3.6 — Secure vhost defaults.** ✅
*Done when:* TLS 1.2+, modern ciphers, HSTS with configurable max-age, OCSP stapling and a 301 HTTP-to-HTTPS redirect are generated by default. **M**

The 301 and HSTS landed in S3.3; this adds the protocol floor, the cipher list, session handling and stapling, and makes the HSTS max-age configurable with zero omitting the header.

OCSP stapling deviates from the wording, deliberately. Let's Encrypt has retired OCSP in favour of CRLs and its certificates no longer carry a responder URL, so `ssl_stapling on` against one makes nginx warn on every reload and staple nothing. Servlo reads the leaf and emits the directives only when a responder is named, which is the only form of "stapling by default" that does anything. A site on an authority that still publishes OCSP gets it.

---

# PHASE 2 — Production mode and the login
*Goal: Servlo can be safely exposed to the internet.*

## E4 — Production defaults

**S4.1 — Production mode switch.** ✅
*Done when:* a mode flag exists in config; enabling is confirmed; disabling requires a force flag; the mode is shown prominently in the dashboard. **M**

The asymmetry is the point. Turning it on is confirmed because it changes what visitors see; turning it off needs `--force` because doing that on a live machine starts showing stack traces to the internet. A flag rather than a prompt, since a prompt can be answered by muscle memory or by a script piping y.

The badge sits in the dashboard header rather than a settings tab, so the state is visible from every page.

**S4.2 — Production defaults across the stack.** ✅
*Done when:* idle-suspend off; worker restart policy `always`; PHP defaults `display_errors=Off`, `expose_php=Off`, OPcache on with `validate_timestamps=0`. **M**

Idle-suspend needed nothing: S0.6 deleted it, so there is no state to set off.

The PHP settings land in a drop-in named `90-production.ini`, which sorts ahead of the shared and per-site files so a site can still override any of them. Writing it happens the moment the flag changes rather than at the next restart, so what is on disk always matches what the flag says.

`ProductionIniFile` was going to live in `internal/phpini` and could not: phpini imports php imports podman, and podman needs the path to mount it. It sits in `internal/config` with the other paths instead.

**S4.3 — Service hardening.** ✅
*Done when:* database and cache services bind to the container network only and never publish to a public interface; passwords are generated strong; a test asserts no service listens on a public address. **M**

Only nginx ever binds beyond loopback. `lan:expose --services` could publish MySQL and Redis to every interface, which contradicts both this story and CLAUDE.md §3.7, so the whole managed-service exposure setting is gone rather than defaulted off: config field, CLI command, TUI rows, dashboard card and the two API actions. A toggle that cannot do anything is worse than no toggle, and the route now refuses the action names it used to answer to. A quadlet arriving bound to `0.0.0.0` is pulled back rather than read as a deliberate operator bind.

Half of that toggle survived this story and should not have. `lan:expose` kept deciding whether nginx bound loopback or every interface, and it defaulted to loopback, so a fresh production install came up serving nobody until somebody found the switch. It carried an explicit carve-out in the surface scan on the grounds that a server needs to choose its bind, which is the part that was wrong: nginx serves the sites and everything else does not, and neither half of that is a decision an operator benefits from making. The whole feature is now deleted, the carve-out is gone, and the scan owns `lan:expose` like it owns the rest. The CI serving check moved with it: `--resolve` against 127.0.0.1 passes on a loopback-bound nginx, so it now asks the runner's own address as well, which is the assertion that would have caught this the day it landed.

Passwords are generated once per install, stored 0600, and substituted wherever a definition writes `{{password}}`. Substitution happens on the raw YAML at load, not field by field at use: the placeholder appears in container environment, site `.env` vars, connection URLs, launch commands and mounted config files, and the first version of this — a replacer inside `Resolve` — reached four of those and missed `Environment`, which is where every database password actually lives. It also skipped single-version presets entirely, which is most of them.

Quote the placeholder when it is a whole YAML value. Bare braces are a flow mapping, so `MYSQL_ROOT_PASSWORD: {{password}}` does not parse at all, which the first pass over the store shipped in nine files.

One password per install rather than one per service, because the definitions cross-reference each other: phpMyAdmin logs into MySQL, RedisInsight into Redis, and a per-service secret needs a resolution pass those definitions cannot express.

Three things this turned up that were not the story. `internal/presetfixtures` carried a copy of the service store for tests, taken at S0.8, and it had already drifted by whole fields; it now serves the real definitions, and one of the differences it was hiding was `LERD_POSTGRES_HOSTS` in pgadmin, silently breaking that preset's family discovery. The scan rule that should have caught it was case-sensitive and is not any more. And `embed/` was an unreferenced copy of the quadlets and units, still carrying the published passwords, so it is deleted.

The database caveat worth remembering: MySQL and PostgreSQL apply a root password once, when the data dir is initialised. Generating a new one does not change an existing database's password. There are no releases yet, so no install needs migrating, but rotating one later is a database operation rather than a file edit.

## E5 — Authentication

**S5.1 — Panel access, both paths.** ✅
*Done when:* at first boot the panel is reachable at `https://<ip>:<port>` with a self-signed certificate; a subdomain can then be attached and receives a real certificate through the normal Get SSL flow. **L**

One port, not two. The listener reads the first byte of each connection: a TLS handshake starts with 0x16, and anything else is answered with a 308 to the same URL over https. A second listener for the redirect would need a second port in every firewall rule and every page of documentation, to serve a response whose whole content is "use the other one". The redirect never reaches the panel's handler, because whatever the plain request carried was already in the clear and answering it with real content or a cookie makes the mistake worse.

The self-signed certificate is a leaf that signs itself, not a local authority. Nothing is installed into any trust store; S3.1 deleted that and it has no successor here. It is reused rather than regenerated, because a fingerprint that changes on every restart teaches an operator to click through warnings, and it is replaced only when the addresses change, a domain is attached, or it nears expiry.

The domain path reuses the site machinery whole: the same ACME challenge location, the same certificate directory (so the renewal scanner finds it without being told), the same TLS defaults. The vhost is written on port 80 alone until a certificate exists, because nginx refuses to start when an `ssl_certificate` file is missing and a panel misconfiguration that stops nginx takes every site with it.

The trap worth recording. Servlo treats requests arriving over its unix socket as local control, bypassing the remote gate entirely, and that was safe because the only thing proxying into that socket was the `servlo.localhost` vhost, which RFC 6761 makes unreachable from any other machine. A panel domain proxies into the same socket and is reachable from anywhere, so attaching one would have handed terminal access, filesystem browsing and raw `.env` reads to whoever typed the URL. The vhost now marks what it forwards and the panel refuses local-control trust to anything carrying the mark. `proxy_set_header` overwrites what the client sent, so it cannot be stripped from outside, and it only ever removes trust, so setting it directly denies only yourself.

Two things fell out of that. The CSRF gate had the same socket shortcut and got the same treatment. And the paused-site holding page carried a Resume button that POSTed cross-origin to the panel on 127.0.0.1 — a page served to whoever visits the site, which on a server is the public. It is a link to the dashboard now, and the CSRF exemption that existed for it is gone.

**S5.2 — Session authentication.** ✅
*Done when:* Argon2id hashing; cookies `HttpOnly`, `Secure`, `SameSite=Strict`; CSRF on every state-changing route; per-IP rate limiting with progressive lockout; sessions listable and revocable from the CLI. **L**

This is where the laptop model finally goes. Upstream trusted loopback absolutely and asked everyone else for HTTP Basic credentials; on a droplet a request from 127.0.0.1 is a reverse proxy or a container, and Basic sends the password on every request with no way to sign out and no way to see who is signed in. Now every request carries a session or it goes no further, whatever address it came from.

Sessions are records rather than a signed cookie, which is the only shape in which "listable and revocable" means anything. The store holds each token's SHA-256, so reading it is not the same as holding every live session, and both the panel and the CLI read through to the file so a session ended from a shell stops working on the next request.

Two acceptance criteria turned out to need more than they say. `SameSite=Strict` means the cookie is never attached to a cross-origin request, and the dashboard was cross-origin with itself: served from `servlo.localhost`, calling `https://localhost:7073`. The `servlo.localhost` vhost proxies `/api/` now and the split-origin arrangement is gone. The origin allowlist stays, because the websocket handshake still checks against it. And CSRF was a header whose *presence* was the proof; it carries a per-session token now, because a marker anyone can set is not a token.

The cookie carries the `__Host-` prefix on top of the three flags asked for. It is a promise the browser enforces — Secure, path `/`, no Domain — and on a panel sharing a parent domain with the sites it hosts, it is what stops a site's own JavaScript planting a session cookie.

Deleted rather than kept: `internal/ui/remote_session.go`, the HMAC-over-the-password-hash cookie, superseded whole. The LAN-exposure gate and the Basic challenge went with it. What is left of the old gate is the cross-origin check on the routes that reach the panel without a session, the source gate on the mailpit webhook, and the host-action restriction — a terminal on the host is not something a password alone should open, and S5.4 and S5.5 replace that with roles and a permission per route.

One bug this introduced and caught: stripping the gate left the mailpit webhook falling through instead of being refused, because its check was written as "if from the host, pass" with the rejection implied by what came after. Its own test found it.

`servlo users` and `servlo sessions`, not `servlo auth`, which upstream already uses for sharing SSH keys with the containers. Two unrelated meanings of the word under one command would be worse than a longer name.

Credentials an install already had become an account at the next start, once, and the inherited bcrypt hash is rehashed to Argon2id the first time it is used. Without that, upgrading would present the first-run setup form on a machine that already had a password.

**S5.3 — Optional TOTP.** ✅
*Done when:* QR enrolment, one-time recovery codes, and a CLI reset path for lockout recovery all work. **M**

SHA-1, six digits, thirty-second step, which is not a choice so much as the only interoperable option: it is what every authenticator app assumes when it scans a QR code, and a stronger digest produces codes no app will generate. The RFC 6238 test vectors are in the tests, because a code Google Authenticator will not produce is a code nobody can enter, and nothing short of the vectors proves the implementation is TOTP rather than something that merely behaves like it.

Nothing is stored until the operator types back a matching code, so an app that failed to scan leaves the account as it was. Both surfaces work that way: the panel and `servlo users totp enable`, which draws the QR in the terminal rather than writing it to a file nobody remembers to delete.

Recovery codes are hashed with SHA-256, not Argon2id. They are uniform randomness with no dictionary to try, so the slow hash buys nothing and would cost 46 MiB per attempt on a box someone can aim attempts at. They are shown once and there is no command to show them again: storing them in a form servlo could reprint would make them a second password on the same disk as the first.

The enumeration bug worth recording. The first version said "that account needs a code" whenever the account had TOTP on, which announced that the account existed and had a second factor — the same enumeration the single failure message avoids, by another route. The store reports an outcome now, and "a code is owed" is only ever returned when the password was right, so it tells the holder of that password something they can act on and everyone else nothing. Its own test caught it.

A missing or wrong code counts against the limiter like a wrong password, or the second factor is six digits an attacker can try a million times.

Two things fell out. `redact` is one function now rather than a line in each of four methods, because a credential field added later has to be stripped in one place rather than remembered in four. And the recovery-code count needed to survive redaction while the hashes do not, since the panel shows how many are left.

**S5.4 — Roles.** ✅
*Done when:* Admin sees everything; Developer sees deploy, logs, files, cron and settings for assigned sites only. Enforced on every route and in every WebSocket message. **L**

Deny by default, which is the decision the rest follows from. A path the classification does not recognise is refused to a developer, so a route added later without a thought for roles breaks for them loudly rather than working for everyone quietly. The cost is real and it is the right one: the alternative fails silently and nobody finds out.

"Every WebSocket message" is the half that would have been easy to skip. The broker builds one snapshot and hands it to every connection, so a developer who could not open another team's site could still watch it — the door beside the door S5.2 already closed once. Frames are filtered per connection at send time, not per role in the broker, because the broker does not know who is listening and a role-keyed cache would be one more thing to invalidate when an assignment changed.

Two details that read as fussy and are not. An absent sites payload stays absent rather than becoming an empty array, because the client reads a missing key as "unchanged" and an empty one as "there are none", so converting would wipe the list on every frame that happened not to carry sites. And a snapshot servlo cannot parse is filtered to empty rather than passed through, or a malformed frame is the way to see everything.

An empty site list on a developer means nothing, not everything. It is the state an account is in the moment it is created, and the other reading makes every new developer an admin until someone notices.

Assignments are a shell command. A developer able to widen their own list would make the role advisory. They take effect on the next request rather than signing anyone out, since every route checks the live account rather than anything the session remembers.

**S5.5 — Permission registry.** ✅
*Done when:* every state-changing route declares a permission and the surface scan fails the build on any undeclared route. **L**

The scan's permission half, which CLAUDE.md has said was unbuilt since S0.4 because there was no registry to audit against. It reads the panel's own dispatch — the mux registrations and the auth middleware's switch, since the login routes have to answer before there is a session for the mux to be reached with — and fails on a route that declares nothing, or a declaration no route registers. The second direction matters as much: a stale entry is a permission somebody will read as describing the panel, and it is how a renamed route quietly loses its own while the count still looks right.

Deny by default already made an undeclared route fail closed for a developer, which is the safe direction. This makes it fail at build time, so the person adding the route finds out rather than the person using it.

One site permission rather than the read/write pair the first draft had. With two roles they are the same check, and a distinction that changes nothing is a distinction to keep in step for no benefit; splitting it belongs to whatever story introduces a role that reads without writing.

The narrowing worth recording. The worker and unit log streams are keyed by container or unit name rather than by domain, so their paths carry nothing to check an assignment against. They are admin until those routes say which site they are about. That is narrower than S5.4's wording implies, and the alternative — declaring site scope on a path servlo cannot read a site out of — would be a leak dressed as a feature. The registry comments say so at the entries themselves.

Three lists became one on the way through. `scope_http.go` had its own developer-path list, `panel_auth.go` had its own public-path list, and the registry is now what both read. Two lists is how they drift, and the one that drifts is always the one doing the enforcing.

The scan found three real gaps the moment it ran: `/api/sites/link` and `/api/sites/reorder` were being read as sites named "link" and "reorder", accidentally safe for the wrong reason, and `/api/logs/terminal` was undeclared entirely.

**S5.6 — Audit log.** ✅
*Done when:* append-only, 0600, rotated, never truncated by the app; records actor, source IP, action, target, result and timestamp; visible in the dashboard. **M**

"Rotated" and "never truncated by the app" look like they pull against each other and do not: rotation is a rename. The live file is only ever appended to, and past four megabytes it is renamed and a fresh one starts, so nothing written is unwritten. Five kept, because rotation that never prunes trades one unbounded file for an unbounded number of them. Reading spans them, or the history vanishes the moment the log grows past its threshold, which is exactly when it becomes interesting.

Not logrotate, which would put the guarantee in a config file servlo does not own, on a schedule servlo cannot see, and an operator who disabled it would get a full disk rather than a rotation. The check happens at the append, on a size already in hand.

Recording is middleware, not a line in each handler. Seventy handlers is seventy chances to forget, and the one that forgets is the one somebody later needs. Reads are excluded, because a log with every page view in it is a log nobody reads; refusals are included, because somebody reaching for what they may not have is most of why the log exists.

The ordering took a correction. Audit started inside `ScopeSites` and so never saw a refusal, since the refusal is written before the inner handler runs. It sits outside now: Require establishes who, Audit records what they tried, ScopeSites decides.

A shell command is attributed to the Unix user running it and carries no IP. On a box where one operator has the login and the rest reach the panel, that is exactly the distinction worth recording, and a loopback address would suggest the entry knows something it does not. No actor at all still means servlo on a timer, which is what a renewal or a self-heal is.

Query strings are never recorded. The file is 0600 and it is also the file an operator pastes into a support thread, which is the same reason `Detail` was already redacted.

**S5.7 — Strip dev-only UI surfaces.** ✅
*Done when:* no Tinker tab, terminal button, profiler view, dump viewer, Xdebug toggle, per-version `php.ini` editor or worktree strip remains anywhere in the Svelte app. **L**

Most of the named list was already gone, deleted by the story that owned each feature. What was left was one family nobody had named yet, and naming it is what made it obvious: three buttons that all did the same thing, which was start a desktop application on the machine running servlo. A terminal emulator tailing a unit, a file manager opened on a site's directory, an IDE opened on a file and a line.

A droplet has no desktop for any of them to appear on, so each spawned a process nobody would ever see. That is the harmless reading. The other one is that each was a way to start an arbitrary process on the box that runs every site, reached from a browser, and the panel is now on the public internet.

The terminal one outlived the container shell drop-in S0.5 deleted, because they look alike and are not: that entered a container, this opened a window. Different features, same fate.

The `terminal` command output went with them, and the runner now refuses an output it does not recognise instead of quietly running it as `text`. The store no longer produces one, but a hand-edited project file still can, and a value that means nothing should say so rather than mean something else.

**S5.8 — `SECURITY.md`.** ✅
*Done when:* the threat model — including the shared-Linux-user tradeoff from PRD §6 — and a disclosure process are written down. **M**

The tradeoff gets its own section rather than a line in a list, because it is the one thing to understand before putting a client's site next to your own: every site runs as the same Linux user, so a site that gets code execution reads every other site's `.env`. Written as what it is, a deliberate trade with two real mitigations and one piece of advice no code can enforce.

The attackers are the ones who actually show up: a scanner finding the login form, a compromised WordPress plugin, a developer with an account reaching a third site, and anyone who ends up with a copy of a file. What is out of scope says so plainly — an attacker with a root shell, a hostile base image, a hostile host — because a threat model that claims to cover those is not one to trust about anything else.

Private vulnerability reporting on GitHub, three working days to acknowledge, credit in the release notes. No bounty, and it says so.

---

# PHASE 3 — Site management
*Goal: everything an operator does day to day, from the browser.*

## E6 — Adding sites

**S6.1 — Add site: existing folder.** ✅
*Done when:* domain, PHP version, auto-detected framework and document root are captured; the directory is created, the vhost generated, the site registered; the Get SSL button appears with its DNS check running. **L**

The inherited flow could not be kept. It shelled out to the servlo binary with the browser's chosen directory as the working directory and let `servlo link` derive the rest, which worked while a site's domain was its directory name plus a TLD servlo resolved itself. Here the domain is given and never derived, so it has to reach the linker as a value rather than as an argv the CLI parses back out. The panel resolves and applies a plan in process now: same linker, same registration, same vhost the CLI takes, differing only in policy.

That policy is the interesting part, and it refuses two things a CLI link allows. A repository's own dev-server command and its inline service containers stay unrun, because a click is consent to serve a project and not to execute code the repository chose, and a browser has no way to ask about that properly. Issuance stays off, because it is gated on a live check that the domain resolves here and a site created a second ago has not passed it. Get SSL is its own button for exactly that reason.

Asking about a path is a separate request from creating a site, and a GET, so the form can show what servlo found in a directory before anything exists. One missing level is created; a missing tree is refused, since that is how a typo becomes a directory nobody looks for again. Every refusal happens before anything is written, so a rejected domain leaves nothing behind on the retry.

The `/api/browse` and `/api/sites/link` routes were on the loopback-only list, which on a droplet means the add-site flow could not be reached from the panel at all. Link is deleted; browse is now what its permission says it is, admin, with a session, CSRF and an audit entry behind it. What remains on that list, databases, raw `.env`, tools, is the same question deferred to the stories that own them.

**S6.2 — Add site: clone from GitHub.** ✅
*Done when:* Servlo generates an SSH deploy key, displays it for pasting into the repository, verifies the connection with a test, then clones. Connection failure gives a specific reason, not a generic error. **L**

Three requests rather than one, and the middle one is the story. Without the test button a clone of a private repository fails with "Permission denied (publickey)" and nothing on screen says that a key needed pasting anywhere, which is the generic error the acceptance criterion names.

A deploy key servlo generates, not the operator's own key, and one per site. A key that is scoped to a repository means a compromised site hands over that repository rather than everything its owner can read, and revoking one site's access is deleting one file rather than working out what else breaks. Servlo generates it rather than asking, because the alternative is a form that asks somebody to paste a private key into a browser. The private half is 0600 next to the other credentials and never appears in a response; its own test asserts that, and the assertion caught a version that returned the path.

Exit status is not how you read a connection test. GitHub answers a perfectly good deploy key with its refusal-of-shell-access banner and exits 1, so reading the code would report every working key as broken. What the host said is the answer.

The five failures each get their own sentence, because each has a different fix: the key is not on the repository, the key is on a different repository, DNS, an outbound firewall, a host key that changed. Anything unanticipated carries ssh's own words, which beats a sentence servlo invented about a failure it does not know.

Whichever of the three URL spellings the clone menu offered is accepted and converted to the SSH form, since refusing the https one would be correct and useless: it is the one GitHub puts first. A URL carrying a token is refused, because that token would land in the site config and the audit log, and the deploy key is what replaces it.

Mutation testing earned its keep here. Four refusals in the URL parser all survived being deleted, because every test input was being caught by an earlier guard; the inputs that actually reach each one found a real hole, a repository path of `../../etc/passwd` that the character class read as a legal owner and name. And the non-empty-directory refusal survived too, because git's own failure for that case also contains the word "empty" and the assertion was matching on it. It asserts servlo's own words now, and that git never ran.

Three bugs found reading back over S6.1 and S6.2, all fixed.

The deploy keys were a pair of files named `<site>` and `<site>.pub` in one directory. `.pub` is a real TLD, so `blog.example` and `blog.example.pub` are both domains somebody can own, and the second site's private key was written straight over the first site's public key file. The first site then read its private key back as its public one, and the panel offered it up to be pasted into GitHub. Each site gets its own directory now, which cannot collide however the domain is spelled and costs an inode. The flat layout had a second, quieter version of the same fault: removing one site's key deleted the other's.

The form's PHP version and document root were taken as given. The version becomes the FPM upstream's name in the generated vhost, so nothing between the form and that template was checking a string that ends up in nginx configuration; the vhost writer's own guard caught a semicolon, which is defence in depth doing input validation's job, and it caught nothing about `8.9` or `nonsense`, which registered a site that could only ever answer 502. The document root was worse in a quieter way: the vhost writer silently replaces one it does not like with `public`, so a site was registered claiming a root it did not serve from. Both are normalised and refused at the handler now, with the reason.

Two of the tests written for those fixes passed before the fix, because the container has no running nginx and any error at all satisfied "was it refused". They assert the specific refusal now. A test that passes on the wrong error is a test that will keep passing after the bug comes back.

**S6.3 — Add site: upload a ZIP.** ✅
*Done when:* the archive uploads, extracts, and the document root is detected. **M**

The extractor is written as a refusal engine with extraction as a side effect, because this is the one place in servlo where a file the operator did not write decides a path servlo writes to. Zip slip, obviously, and also the backslash spelling of it, which is a path separator on the machine that wrote the archive and an ordinary filename character to a check looking only for slashes. Symlinks are refused rather than resolved: a link at `/etc/passwd` turns a file the site serves into a file the machine owns, and no site needs one badly enough to be worth checking its target.

Modes from the archive are dropped. Everything lands 0644 and directories 0755, because every site here runs as the same user and an execute bit in a zip is a decision somebody else made about a file on this machine.

Extraction goes to a scratch directory beside the target and is moved in only once the whole archive has been read, so a refusal leaves the site directory as empty as it found it. The handler takes back a directory it created, the way the clone path does, so servlo's own leftovers never block the retry.

Everything a forge's "Download ZIP" produces is wrapped in one directory named for the branch, and extracting that verbatim gives a site whose document root is one level below where anyone would look. One top-level directory is unwrapped; two or more are left alone, since moving either would be inventing a structure the archive did not have.

Eleven mutations, ten killed. The eleventh is the per-entry byte cap, and it survives honestly: `archive/zip` validates the stream against the declared size itself and refuses an entry that lies before servlo's counter sees a byte of it. The check stays as three lines that do not depend on that staying true, and the comment says so rather than implying it is load-bearing today. Getting there took separating the two size guards, which had been covering for each other: the total is checked from the declared sizes before anything is written, and the per-entry bound is only about one entry against the ceiling.

Two tests were wrong before they were right. The mode test built its archive with `w.Create`, which ignores the header, so it asserted nothing at all. And the lying-archive test asserted servlo's own error message for a case `archive/zip` catches first; it asserts the property that matters now, which is that the archive is refused and the directory is left clean.

A fourth bug turned up while writing this, and it was in S6.2 as well. A failure *after* the files land, the linker refusing or the vhost failing to write, left a directory full of servlo's own work and no site registered, and the operator's retry was then refused as "not empty" by servlo's leftovers. Both flows require the directory to be empty before they start and check it, so everything in it afterwards was put there seconds ago and rolling it back cannot take anything of the operator's. A directory servlo created goes entirely; one the operator made is emptied and left standing, because taking it would be removing something they chose to have.

And CI found a fifth, in the tests rather than the code. Two of them drove the handler all the way through to a real registration, which on a machine that has podman means building a PHP image: the package went past Go's ten-minute timeout on the runner, having spent it compiling PHP extensions. The refusals can go through the handler because they return before any of that; the acceptances are tested against the validation function directly now.

Chasing that turned up the ordering it was hiding. The overrides were checked after the clone, so refusing a PHP version cost a four-second network round trip first and left a directory to roll back, which is the opposite of the "refuse everything refusable before anything happens" the comments claimed. Checking and applying are two steps now: checked up front with the domain and the path, applied once there is a plan to apply them to. The clone-refusal test went from four seconds to nothing. The one test that does still want a failing clone names a host that cannot resolve, rather than depending on whether the machine running it can reach GitHub, and `GIT_SSH_COMMAND` grew a connect timeout so a dropped connection cannot hang a panel request for as long as the kernel keeps retrying.

One papercut in the modal while it grew a third source: switching source left the previous one's error on screen, where it described something the operator was no longer doing.

**S6.4 — Add site: app installer.** (engine, definition, setup and install done; panel wiring blocked on S11.4)
*Done when:* a fresh WordPress installs in one click — database created, `wp-config.php` written, admin account set up. The app is defined as **store YAML with no Go code specific to it**, so further apps need no release. **L**

The store had a directory and an empty index and nothing else, so this is the engine as well as the first definition. Split because the criterion has two halves that fail differently: fetching, verifying and configuring is one shape of work, and driving an application's own setup flow to create an admin account is another. This is the first half.

Verification is the part worth being strict about. The definition pins a version and a sha256, and both are refused at parse time rather than at install time; a source URL that is not https is refused too, because the checksum only helps if the definition carrying it arrived intact. Nothing is written into the site until the whole download has been read and its checksum has matched, which is why the release is held in memory rather than streamed to disk. The archive is a zip so it goes through the extractor S6.3 hardened, rather than a second unpacker with its own oversights.

The wrapper directory is named in the definition rather than inferred. Inference is right for an operator's upload, where servlo has no idea what is in the archive; for a pinned release, a version that stops shipping a wrapper should fail loudly rather than quietly install a directory deeper than the vhost expects.

Salts are the case that proves the law. WordPress wants eight independent keys, and the engine knows only that a definition asked for eight named random values of a given length. The alphabet they are drawn from excludes quotes, backslashes and newlines, because the values are substituted into a config file the application then executes, and generating a value the renderer would refuse is a bug waiting for a one-in-a-hundred install to find it.

The checksum was cross-checked against the SHA-1 wordpress.org publishes beside the release, so it is verified against upstream rather than computed from a single download and trusted.

Best thing in this story is the law test. The skill for this store says plainly that the surface scan does not catch "no app name in Go" and that review does, which is a person remembering rather than a mechanism. There is a mechanism now: every app the store ships is looked for in every Go file in the package. Its first version used word boundaries and a mutation proved that useless, since `_` is a word character and `wordpress_config` is exactly the shape the law forbids. It matches anywhere now, and it immediately caught two things I had written myself, a test fixture named for the app and a framework field carrying a real framework name.

The admin account is the application's own installer, driven once over HTTP. Writing WordPress's user table from Go would be app-specific and wrong the first time that schema changes, so the definition describes the form: where it posts, what goes in it, and what the answer has to say. The path must be site-relative, because that request carries an admin password generated seconds earlier and a definition able to name a host would be choosing where to send it. A response has to say it worked, or a setup that silently did not happen leaves an uninstalled application on a live domain for the first passer-by to claim. A failure reports what the application said with every value servlo put into the request removed from it, since an application echoing its own form back into an error page is not hypothetical.

A second guard there looked prudent and was unreachable: once the path starts with a single slash, Go's parser keeps the host as the site's whatever the path spells, backslashes and encoded slashes included. A mutation showed it by surviving, and it is gone rather than shipped as a branch no test can reach.

The install order is the design. Each step is undoable only by the step that has not happened yet, so the expensive and fallible parts run first: fetch and verify, then the database, then the config file. A release that fails its checksum therefore leaves no orphaned database behind, and that has its own test rather than being left as a property of the reading order. Registering the site and driving the setup form come last, because those are the two that need the site to be servable, and splitting there keeps the install function honest about what it can promise.

A database is a Connection rather than an assumption about a local container, so an app installed against an external managed database goes down the same path (PRD §5.9). An app declaring it needs one and given no way to make it is refused rather than quietly writing a config file pointing at nothing.

The panel wiring stops here, and writing it is what showed why. `serviceops` can create a database and nothing else: there is no per-site user with a password, because that is S11.4. So the install handler had a connection carrying a name and a host and an empty password, and the renderer accepted it, and the config file would have gone to disk with `DB_PASSWORD` blank. Either the site cannot connect, or it connects as whoever needs no password.

The renderer refuses a blank credential now, which turns that from a live site with an empty password into a refusal at the point of writing. The handler itself is not in the tree: an app install that cannot give the app a database it can reach is not an app install, and shipping the route with the hole in it would have been the third silent-wrong-value bug of the session. S6.4 finishes when S11.4 does.

## E7 — Site settings

**S7.1 — Per-site PHP-FPM pool.**
*Done when:* each site has its own pool, so PHP settings apply per site rather than per PHP version. **XL**

**S7.2 — Combined settings fields.**
*Done when:* **Max upload size** writes PHP `upload_max_filesize`, PHP `post_max_size` and nginx `client_max_body_size` together; **Max execution time** writes PHP `max_execution_time` and nginx `fastcgi_read_timeout`/`fastcgi_send_timeout` together; **Memory limit** writes `memory_limit`. A test uploads a file larger than the old limit and asserts it succeeds after one field change. **L**

**S7.3 — Per-site PHP version switching.**
*Done when:* changing the version regenerates the vhost and pool and reloads without downtime. Inherited; verify against per-site pools. **M**

**S7.4 — Nginx settings, friendly and raw.**
*Done when:* common needs are form fields (upload size, timeouts, headers, static caching); a raw editor sits underneath; every save runs `nginx -t` first and keeps a timestamped backup with one-click restore. **L**

## E8 — Domains

**S8.1 — Aliases.** Multiple domains on one site, included in certificate SANs and the DNS pre-flight. **M**
**S8.2 — www-to-non-www** as a toggle, in either direction. **S**
**S8.3 — Subdomains as independent sites.** **M**
**S8.4 — Redirects,** whole-domain and URL-level, managed from the panel. **M**

## E9 — Deploy

**S9.1 — Production profiles in the framework store.**
*Done when:* Laravel, WordPress and plain PHP templates exist as store YAML declaring build commands, migration command, exclude list and health path. **No framework name appears in Go.** **L**

**S9.2 — Editable deploy script per site,** pre-filled from the framework template. **M**

**S9.3 — Deploy.**
*Done when:* `git pull`, then the deploy script, then a graceful PHP-FPM reload; output streams live into the panel; a database backup runs automatically first when the script contains a migration. **L**

**S9.4 — WordPress exclude list.**
*Done when:* `wp-content/uploads` and `wp-content/plugins` are excluded by default and the list is editable; a test installs a plugin, deploys, and asserts the plugin survives. **M**

**S9.5 — Redeploy previous commit.**
*Done when:* one click checks out the prior commit and re-runs the script; the UI states plainly that database migrations are not undone. **M**

**S9.6 — Deploy history:** commit SHA, author, duration, outcome, who triggered it. **M**

**S9.7 — Git webhook deploy:** signed, per-site secret, branch filter, replay protection, off by default. **L**

## E10 — Node and asset builds

**S10.1 — Per-site Node version.** Inherited; verify against production paths. **M**
**S10.2 — Memory-capped build scope.**
*Done when:* asset builds run in their own scope with a memory cap, so an oversized build fails alone rather than triggering the OOM killer against MySQL. A test runs a deliberately oversized build and asserts other services survive. **L**
**S10.3 — Pre-build warning** when the droplet is small relative to the build. **S**

## E11 — Databases

**S11.1 — Database as a connection.**
*Done when:* install-time and per-site choice between local MySQL/MariaDB, local PostgreSQL, and an external managed database. **L**

**S11.2 — Managed database support.**
*Done when:* host, port, credentials and CA certificate are stored; the connection is tested; the database and a least-privilege user are created remotely; values are written into `.env`; **the droplet's public IP is displayed for pasting into DigitalOcean trusted sources.** **L**

**S11.3 — Admin UIs.** phpMyAdmin auto-installed with MySQL/MariaDB, pgAdmin with PostgreSQL, each pointed at whichever connection the site uses. **M**

**S11.4 — Per-site database user** with schema-scoped grants, and rotation. **L**

## E12 — Cron

**S12.1 — Cron UI** per site: command, schedule, output capture, last-run status, built on systemd timers. **L**
**S12.2 — Real WordPress cron.** WP pseudo-cron disabled, a one-minute system cron installed in its place. **M**

## E13 — Email

**S13.0 — Delete Mailpit.** ✅
Mailpit is on the deleted list (PRD §4.2) but nothing in Phase 0 removed it, so the preset, the framework stores' `MAIL_*` wiring, the dashboard card and the docs all still ship it.
*Done when:* the service preset, its env mappings in every framework definition, the UI and the documentation are gone, and the surface scan rule that names it is enforced. It sits here rather than in Phase 0 because per-site SMTP is what replaces it: a local mail catcher is a development convenience, and deleting it before there is anywhere for mail to go would leave a site with no mail story at all. **M**

The rule names `mailhog` as well as `mailpit`. The Sail importer translated one into the other, so leaving the old name behind would have let the catcher back in under the alias it arrived by.

The notification kind went with it. `mail` was a category an operator could toggle under System → Notifications, and the mailpit webhook was the only thing that ever produced one, so it and its locale strings came out too rather than staying as a switch that controls nothing.

**S13.1 — Per-site SMTP** written into `.env` or `wp-config.php`, with a **Send test email** button. ✅ **M**

Store-first, the same shape the database credentials already use: the framework definition declares which keys carry mail under `env.smtp`, and `sitetpl` fills `{{smtp_host}}` and the rest from the site's own account. Laravel gets seven `MAIL_*` keys, Symfony gets one `MAILER_DSN`, WordPress gets constants in `wp-config.php`. Magento declares none on purpose: its mail settings live in the database, not in `env.php`, and the card says so rather than writing keys the application ignores.

Three spellings of the encryption setting exist because frameworks do not agree on one. `{{smtp_encryption}}` is Laravel's `tls`/`null`, `{{smtp_crypto}}` is the bare form CodeIgniter and the WordPress plugins want, and `{{smtp_tls}}` is the boolean a DSN query parameter takes. There are URL-encoded credential placeholders too, because a password with an `@` in it truncates the host out of an unencoded `smtp://user:pass@host`.

The test button sends a real message and returns the mail server's own reply. That is the whole point of it: `550 5.7.1 Sender address not verified` is actionable and "could not send" is not.

**S13.2 — Panel SMTP** for alerts, configured separately. ✅ **S**

This closes S3.5's email half. A renewal that starts failing emails the operator once, with the authority's reason, and not again until a success has cleared the record: an identical email every hour for three weeks is a filter rule rather than an alert.

Alerts go to the account's own sender address. A separate destination field would be a second thing that can disagree with the first, and an alert delivered to the wrong one is the failure the alert existed to prevent.

The transport lives in `internal/mailsend` rather than beside either caller. `internal/sitesmtp` reaches half the tree to resolve a framework's env keys and `internal/certs` is already underneath that half, so one shared dialer is what lets both send without a cycle.

## E14 — File access

**S14.1 — SFTP per site,** locked to that site's directory. **L**
**S14.2 — File manager:** browse, edit, upload, unzip, fix permissions. Carries an explicit live-site warning; every change is audited; permission-gated. **XL**

---

# PHASE 4 — Operations
*Goal: the difference between "it works" and "you can sleep."*

**S15.1 — Backup a site:** files plus database, compressed and encrypted. **L**
**S15.2 — Scheduled backups** as systemd timers with daily/weekly/monthly retention. **M**
**S15.3 — Test restore:** restore into a scratch database, verify, tear down. Runs on a schedule, not just on demand. **L**
**S15.4 — Destinations:** S3-compatible (covers DigitalOcean Spaces and Amazon S3 in one driver) and SFTP. **L**
**S15.5 — Servlo state and site registry included** in backups. **M**

**S16.1 — Server rebuild.**
*Done when:* a full restore onto a fresh droplet brings back every site, setting, cron entry and database. The UI states plainly that certificates are reissued rather than restored, and that managed databases need the new IP added to trusted sources. A CI job performs a real rebuild onto a clean VM and asserts every site serves. **XL**

**S17.1 — Firewall management** (ufw) from the dashboard. **L**
**S17.2 — Cloud firewall visibility:** detect and display whether DigitalOcean's firewall is also filtering, so a blocked port is debugged at the right layer. **M**
**S17.3 — fail2ban status and unban** from the panel. **M**
**S17.4 — SSH key management:** add and remove authorised keys for team members, additively. Servlo never disables password authentication. **M**

**S18.1 — Log rotation** with configurable retention for nginx, PHP-FPM, workers and application logs. **M**
**S18.2 — Alerts in the panel:** site down, certificate renewal failed, backup failed, deploy failed, worker down, disk filling. **L**
**S18.3 — Email alerts** once panel SMTP is configured. **M**
**S18.4 — Uptime check** per site against the framework's declared health path. **M**

**S19.1 — Hardening audit:** firewall state, open ports, fail2ban, unattended-upgrades, config file modes, any site running with debug enabled. Safe fixes applied on request; anything needing sudo is printed, never executed. **L**
**S19.2 — Reboot resilience CI job:** reboot a VM, assert every site, service and worker returns unaided. **M**

---

# PHASE 5 — v1.1
*Driven by real use, not speculation.*

**S20.1 — Staging sites:** own database, own certificate, `noindex` headers and password protection. **XL**
**S20.2 — Refresh staging from live:** copy live files and database across on demand. **L**
**S20.3 — Import an existing live site:** files plus a `.sql` dump, with the vhost and settings generated. **L**
**S20.4 — More apps in the `stores/apps/` store,** each as YAML with no code release. **M**

**Later, only if real use demands it:** atomic releases with true rollback · per-site Linux user isolation · Prometheus metrics · managing more than one server from a single panel

---

## Delivery plan

| Phase | Outcome | Gate |
|---|---|---|
| **0** | Clean, stripped, Ubuntu-only Servlo binary | Surface scan clean |
| **1** | A real site on a real domain with a real certificate | Live HTTPS site on a real droplet |
| **2** | A panel safe to expose to the internet | Surface scan and permission audit clean |
| **3** | Everything an operator does day to day, from the browser | A site added, configured and deployed without touching a terminal |
| **4** | Backups, rebuild, firewall, alerts | A droplet destroyed and rebuilt from backup, every site live |
| **5** | Staging and site import | — |

Phases 0 through 4 are the shippable v1: roughly **four to five months** for one focused developer, or **two and a half to three** for two.

Phase 4 is not optional and should not be deferred. A panel that hosts client sites without verified backups is a liability, not a product.

---

## Definition of Done — every story

- Failing test written first; behaviour changes update existing tests.
- Surface scan and permission audit still green.
- Behaviour lives in store YAML wherever the design laws say it should; no framework name in Go.
- Documentation updated before the commit.
- Full local gate green: build, `go test ./...`, `go vet ./...`, `gofmt -l` empty, UI tests if the UI changed, installer tests if the installer changed.
- CI green on the Ubuntu 24.04 runner, then merged.

The droplet smoke test is **not** a per-story gate. It happens once, against the finished product, after the last phase lands. A story is done when its tests and CI are green.
- Conventional-commit subject, prose body, files staged by explicit path, no generated-by footers.
