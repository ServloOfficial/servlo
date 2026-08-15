# HANDOVER.md — the things only you can do

Every story in Phases 0 through 5 is written, tested and merged. What is left
needs either money, a machine, or a decision, and none of those can happen from
inside a coding session. This file is the whole list.

Read §1 first. It changes what the rest of the repository's rules mean.

---

## 1. GitHub Actions is off, and the build no longer waits for it

The account has no Actions billing, so every job fails to provision in a couple
of seconds: no logs, no runner, zero billable time. Thirteen attempts across
five workflow runs and three commits produced identical results. It is a
standing condition, not an incident.

**What this changes.** `CLAUDE.md` used to say a story was done at "local green,
then CI green, then merge". It now says the local gate is the gate. A PR body
should say the local gate passed, never imply CI did.

**Run the gate yourself before any merge:**

```bash
make build-ui                          # if the UI changed
go build ./cmd/servlo
go test ./...
go vet ./...
test -z "$(gofmt -l .)"
make test-ui                           # if the UI changed
make surface-scan                      # the deleted-feature gate
cd internal/ui/web && npx svelte-check --threshold error   # expect 0
```

Two Go tests fail in a container and pass on a real machine, so do not chase
them: `TestHandleServiceTuningReset_NoOpWhenMissing` needs dbus, and
`TestHandleWorkspaceLayoutRollsBackAndReportsTheReorderError` needs to not be
running as root. Under parallel load some Vitest files time out at the default
5s; re-run with `--testTimeout=30000` and they pass.

**What you lose without Actions**, and therefore what is unverified:

| Job | What it proved |
|---|---|
| Build & Test | the gate above on a real Ubuntu 24.04 VM, not a container |
| Installer Tests | `tests/installer/installer.bats` against `install.sh` |
| Everything comes back on its own | stop every unit, start `default.target`, assert sites serve with nobody running `servlo start` |
| The server that is about to be lost | a real backup of a real site with a real database |
| A fresh machine plus the backups is the old machine | restore onto a runner that never saw the first, assert every site serves and its settings came back |

The last three are the reboot and rebuild guarantees. They are the ones worth
re-running by hand on the droplet (§3), because a unit test cannot make those
claims.

**To turn it back on:** pay for Actions, then push anything. The workflows are
already in `.github/workflows/` and nothing about them needs changing. Revert
the gate note in `CLAUDE.md` §5 step 5 when you do.

---

## 2. Publish the PHP base images

**Status: not published. Optional. Installs work without it.**

Servlo runs each PHP version in a container built from the official
`php:<version>-fpm-alpine` with about twenty extensions compiled in. Compiling
those is the slow part of an install, so the same image is built once and
published, and the client pulls it instead. On a miss it builds locally, prints
why, and carries on. Same image either way; minutes instead of seconds.

The images used to come from the upstream project's namespace. They now publish
under yours, so nothing in the tree carries the upstream name, and nothing is
published yet because that needs Actions or a machine with a container engine.

**With Actions:** it is automatic. `base-images.yml` fires on any push to `main`
touching the Containerfile. Note that GitHub only offers `workflow_dispatch` for
workflows on the **default branch**, so the file has to be on `main` before you
can trigger it by hand.

**Without Actions**, from any machine with podman or docker:

```bash
export GHCR_TOKEN=ghp_...          # a token with write:packages
scripts/publish-base-images.sh     # all seven versions, this architecture
```

It builds each version, checks the image loads at least 50 modules and that
composer runs, pushes, then prints the `manifest create` commands that join the
architectures into the tag the client actually pulls. Run it once on an x86
machine and once on an ARM one, then the manifests. One architecture alone is
fine: machines of the other kind build from source, which is what every machine
does today.

**The tag is the recipe.** It is a truncated SHA-256 of the Containerfile with
the three customisation placeholders emptied, and it must match
`baseContainerfileHash` in `internal/podman/build.go` byte for byte. The script
and the workflow both compute it the same way. Change the Containerfile and the
tag changes, which is the point: a client only ever pulls an image built from
the recipe its own binary carries. Today's tag is `0db4a0b5cdaa`.

**If you make a GitHub organisation later**, every runtime URL follows one
line: `mainRepo` in `internal/origin/origin.go` carries the release feed, the
installer, all three stores, the changelog and this image namespace. The Go
module path is a separate thing and a separate sweep, so the move as a whole is
not one line. `MIGRATION.md` has the full plan and the ordering that makes it
cheap.

---

## 3. The droplet pass

**Status: never run. This is the real remaining work.**

Deferred by your decision to happen once against the finished product rather
than per story. All five phases have landed, so it is now the only thing between
here and a v1 you would put a client on.

Take a fresh Ubuntu 24.04 droplet, 2GB or more, and a domain you can point at
it.

```bash
curl -fsSL https://raw.githubusercontent.com/realrashid/servlo/main/install.sh | bash
```

The installer refuses anything that is not Ubuntu 24.04, and refuses 22.04 with
Podman below 4.5 rather than half-installing. It will print sudo commands for
the privileged steps instead of running them; that is deliberate, and Servlo
never runs sudo itself.

Then work down this list. Each line is a claim the test suite cannot make.

**Serving and certificates**
- [ ] A site answers on the droplet's **public** IP, not just loopback. CI only ever checked the runner's own address, and a loopback-bound nginx passes that.
- [ ] `servlo doctor` says the port strategy still holds, and still says so after a reboot.
- [ ] Point a real domain, wait for DNS, click **Get SSL**. A real Let's Encrypt certificate issues over HTTP-01.
- [ ] The button stays disabled while DNS does not resolve here, and says what it is actually seeing.
- [ ] HTTP redirects to HTTPS and the HSTS header is present.

**Backups, and the promise that rests on them**
- [ ] `servlo backup <domain>` produces an archive containing files and a database dump.
- [ ] `servlo backup verify --latest <domain>` restores into a scratch database, verifies and tears down.
- [ ] Add an S3 destination (DigitalOcean Spaces or Amazon S3) and confirm an upload **actually arrives in the bucket**. This runs rclone in a container and has never talked to a real endpoint.
- [ ] Same for an SFTP destination.
- [ ] `servlo backup state`, then rebuild onto a second fresh droplet: import the key, restore the state, restore each site, start. Every site serves its own page with its settings intact. This is S16.1's whole claim.

**The things containers hide**
- [ ] Log rotation against an application holding its log file open across requests. Rotation renames rather than copies, and that is the case it is chosen for.
- [ ] The cloud metadata service answering for real, so the Security page links to the right provider's firewall screen. It has only ever seen a stub.
- [ ] Reboot the droplet. Every site, service and worker returns with nobody running `servlo start`.

**Apps and deploys**
- [ ] `servlo apps install wordpress --domain <d>` end to end against the live release: database created, `wp-config.php` written, admin account created, and you can log in.
- [ ] Joomla and Grav install and hand over at their own setup step, which is what their definitions promise. Nothing claims a one-click finish they do not deliver.
- [ ] A deploy with a migration in its script takes a database backup first.
- [ ] A WordPress deploy leaves `wp-content/uploads` and `wp-content/plugins` untouched. Losing a client's media is the failure this exists to prevent.

**Panel**
- [ ] Sign in over HTTPS from another machine, with TOTP on.
- [ ] A Developer account sees only its assigned sites and cannot reach an Admin route, over both HTTP and WebSocket.
- [ ] Every state-changing action shows up in the audit log.

Anything that fails here is a real bug. Send me the output and I will fix it.

---

## 4. Decisions waiting on you

**Make the repository public.** While it is private, `raw.githubusercontent.com`
returns 404 for the store fetch and every install runs on the copy embedded in
its binary. That works, and it is the documented fallback, but it means a
definition published today does not reach an installed binary until that binary
is rebuilt. Going public switches the update path on with no code change.

**Cut the first release.** The version is `0.1.0` and the panel shows a BETA
chip beside it. `CHANGELOG.md` has a Servlo-starts-here entry with nothing under
it. Tagging `v0.1.0` runs `release.yml`, which needs Actions.

**The organisation, and going public.** Both are planned in `MIGRATION.md`,
and they belong together: going public is what switches the store update path on
and what gives this repository runners again, since GitHub provides those free
to public repositories. Doing it before the first release is what keeps it
cheap.

---

## 5. Deliberately not done

From PRD §10, deferred on purpose. Do not let anyone talk you into these before
real use asks for them: atomic releases with true rollback, per-site Linux user
isolation, Prometheus metrics, managing more than one server from one panel.

Also settled and not up for revisiting: every site runs as the same Linux user
(PRD §6), SSH password authentication is never disabled, and there is no mail
server, only SMTP settings.
