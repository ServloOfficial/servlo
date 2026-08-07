# Security

Servlo runs on a machine that faces the internet and holds every credential
your sites use. This document says what it defends, what it deliberately does
not, and how to tell us when we got something wrong.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting on
[realrashid/servlo](https://github.com/realrashid/servlo/security/advisories/new).
It reaches the maintainers without the report being public first, which is what
you want if the finding is real.

Please do not open a public issue for a suspected vulnerability. If private
reporting is unavailable to you, mail <realrashid05@gmail.com> and say in the
subject line that it is a security report.

Include what you would want to receive: the version (`servlo --version`), what
an attacker can do, and the shortest sequence of steps that shows it. A working
proof of concept is welcome and never required.

You should get an acknowledgement within three working days. We will tell you
what we think the severity is and roughly when a fix will land, and we will
keep telling you as that changes. When the fix ships you get credit in the
release notes unless you would rather not have it.

Servlo is one person's project. Nothing here is a bug bounty, and there is no
money behind it. What there is, is a maintainer who will read your report
properly and act on it.

### Supported versions

The latest release is the supported one. Fixes land there, not on older tags.
Servlo has not reached v1.0 yet, so there is no long-term support branch to
promise anything about.

## What Servlo is defending

One operator, or a small team, running many PHP sites on one Ubuntu droplet.
The panel is on the public internet, the sites are on real domains, and the
credentials that matter are the ones in each site's `.env`.

The attackers worth designing against are the ones who actually show up:

- **Anyone on the internet finding the panel.** Scanners find a login form
  within hours of a host being reachable. It is the most probable attack and
  the one the panel spends the most effort on.
- **A compromised site.** An outdated WordPress plugin or a vulnerable
  dependency gets code execution as the web user. This is the most probable
  serious compromise, and the one the tradeoff below is about.
- **A developer with an account.** Somebody legitimately given access to two
  sites who ends up able to touch a third, or to reach an admin-only action.
- **Anyone who gets a copy of a file.** A log pasted into a support thread, a
  backup left on a laptop, an API response in a browser's network tab.

Out of scope, honestly: an attacker who already has a root shell on the
droplet, a hostile Ubuntu package or PHP base image, and a hostile hosting
provider with access to the disk. Nothing Servlo does survives any of those,
and pretending otherwise would be a lie in a document meant to be trusted.

## The accepted tradeoff: all sites run as the same Linux user

This is the one thing to understand before putting a client's site next to
yours.

Every site on a Servlo server runs as the same Linux user. A site that gets
code execution can read every other site's `.env`, and therefore every other
site's database credentials. There is no per-site Linux user, no per-site
filesystem boundary, and no root broker enforcing one.

This is the model most single-operator panels ship, and for your own sites and
your clients' sites on your own droplet it is a reasonable trade. It is worth
knowing rather than discovering.

Two mitigations ship in v1 and both are real:

- **Per-site database users, scoped to their own schema.** A leaked `.env` buys
  the attacker that site's database, not every database on the machine.
- **`.env` files are 0600 and redacted everywhere** they would otherwise
  surface: logs, deploy output, API responses.

And one piece of advice no code can enforce: do not co-locate a site you do not
control with a site that matters. If you host something you cannot vouch for,
give it its own droplet.

A per-site FPM pool running as its own Linux user is a plausible later
hardening. It is not in v1, and no configuration flag turns it on today.

## What is defended, and how

**Panel authentication.** Passwords are hashed with Argon2id at OWASP's
recommended parameters, and a hash Servlo cannot parse is a failed login rather
than an accepted one. Sessions are server-side records, so they can be listed
and revoked; the cookie carries only a token, and the store keeps a hash of it.
The cookie is `__Host-` prefixed, `HttpOnly`, `Secure` and `SameSite=Strict`.
Failed logins are rate limited per IP with a lockout that doubles and caps.
TOTP is optional, with single-use recovery codes, and turning it on never
changes what a wrong password says, so the login form cannot be used to
enumerate accounts.

**Authorisation.** Every route declares a permission, and a route that declares
none fails the build rather than shipping open. There are two roles: Admin, and
Developer scoped to assigned sites. The scope is enforced on every route and on
every WebSocket message, because a panel that filters the REST API and not the
socket is a panel that leaks the socket.

**CSRF.** Every state-changing request carries a token bound to the session,
checked on the server. Cross-origin requests do not get one.

**Transport.** The panel is served over TLS. Sites get certificates from
Let's Encrypt over ACME, and issuance is gated on a live DNS check that every
domain and alias resolves to this server, so Servlo never asks a CA for a
certificate on a domain that does not point here. Secured sites get HSTS and a
redirect. Renewal failure is loud: a dashboard banner, an audit entry, and an
email when panel SMTP is configured. A site never silently serves an expired
certificate.

**Services.** MySQL, Redis and the rest bind to the container network only.
Nothing publishes to a public interface, and passwords are generated, not
defaulted. Only reviewed store presets can start a container; a project's own
config file cannot define one.

**The audit log.** Every state-changing action is recorded, including the
refused ones, which are usually the interesting ones. The file is 0600, append
only, and rotated by rename rather than truncation, so nothing written is ever
unwritten by the application. Query strings are never recorded, because the log
is also the file an operator pastes into a support thread.

**Privilege.** There is no permanent root process. Servlo never runs `sudo`
from a tool call: where privilege is genuinely required, the installer prints
the exact command for a human to run. Ports 80 and 443 come from a single
sysctl set once at install and verified afterward by `servlo doctor`.

**Deleted rather than disabled.** A long list of upstream developer features —
an in-browser PHP REPL, a container shell drop-in, a profiler, a debug-output
bridge, Xdebug toggles, host desktop launchers, an MCP server — is removed from
the codebase, not hidden behind a flag. A CI surface scan enumerates the binary
and the API routes on every build and fails if any of them reappears. A feature
that cannot be reached by any configuration is the only kind that is reliably
off.

## What Servlo does not do

**It does not disable SSH password authentication.** Servlo adds authorised
keys and configures fail2ban, and it leaves password login alone. Locking
yourself out of your own droplet is a worse outcome than the risk this closes,
and it is your decision to make, not the panel's.

**It does not run a mail server.** SMTP settings per site and for the panel are
the entire email story.

**It is not multi-tenant hosting.** See the tradeoff above. If you need real
isolation between tenants, you need something other than Servlo.

## Hardening worth doing yourself

None of this is Servlo's to enforce, and all of it helps:

- Put an SSH key on the droplet and use it, even though password login stays
  available.
- Turn on TOTP for every panel account.
- Give a developer the sites they need and no more.
- Keep the panel on its own domain with a certificate, rather than reaching it
  by IP.
- Read the audit log occasionally. It is the only thing that tells you what
  happened while you were not looking.
