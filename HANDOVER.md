# HANDOVER.md — the things only you can do

Every story in Phases 0 through 5 is written, tested and merged. What is left
needs either money, a machine, or a decision, and none of those can happen from
inside a coding session. This file is the whole list.

Read §1 first. It changes what the rest of the repository's rules mean.

---

## 1. GitHub Actions is on, and it is the gate again

It was off: the account had no Actions billing, so every job failed to provision
in a couple of seconds with no logs and zero billable time. That is over. Jobs
provision, run and report, and CI is the gate for a merge again, alongside the
local one.

**Run the local gate before you push,** because a red runner costs ten minutes
and a reviewer's attention:

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
running as root. Both are green in CI, which is how you tell them from a real
failure. Under parallel load some Vitest files time out at the default 5s;
re-run with `--testTimeout=30000` and they pass.

**What runs on a push**, and what each job is actually for:

| Workflow | Job | What it proves |
|---|---|---|
| `ci.yml` | Build & Test | the gate above on a real Ubuntu 24.04 VM, not a container |
| `ci.yml` | Installer Tests | `tests/installer/installer.bats` against `install.sh` |
| `verification.yml` | PHP, its image, and the database behind it | the shims reach the container, the extensions are there, production mode moves all four settings, and PHP reaches MySQL through nginx and its own pool |
| `verification.yml` | A one-click application install ends in a login page | WordPress installed end to end on a domain that resolves nowhere |
| `resilience.yml` | Everything comes back on its own | stop every unit, start `default.target` alone, assert sites serve with nobody running `servlo start` |
| `resilience.yml` | A site gets a real certificate from a real authority | HTTP-01 against Pebble on a genuinely resolvable name, plus a renewal that revalidates |
| `resilience.yml` | The server that is about to be lost | a real backup of a real site with a real database |
| `resilience.yml` | A fresh machine plus the backups is the old machine | restore onto a runner that never saw the first, assert every site serves and its settings came back |

A container is not a droplet, so §3 says what is still left after all of that.

---

## 2. Publish the PHP base images

**Status: not published. Optional. Installs work without it.**

Servlo runs each PHP version in a container built from the official
`php:<version>-fpm-alpine` with about twenty extensions compiled in. Compiling
those is the slow part of an install, so the same image is built once and
published, and the client pulls it instead. On a miss it builds locally, prints
why, and carries on. Same image either way; minutes instead of seconds.

The images used to come from the upstream project's namespace. They publish
under yours now, and **they are published**: `base-images.yml` ran on
2026-09-06 and the Containerfile has not changed since, so a droplet install
pulls rather than compiles. There is nothing to do here unless that file
changes.

**It is automatic from here.** `base-images.yml` fires on any push to `main`
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

**Status: never run, and shorter than it was.**

Deferred by your decision to happen once against the finished product rather
than per story. All five phases have landed.

What changed is that a good deal of this list is no longer a claim only a
droplet can make. Actions is on, and three workflows now run the product on a
real Ubuntu 24.04 machine with rootless podman, systemd and a lingering user:
`ci.yml` (build, tests, installer bats), `verification.yml` (PHP, its image, the
database behind it, and a one-click install) and `resilience.yml` (boot, a real
certificate from a real authority, and a rebuild from backups).

### Already proven on a real machine

Not on a droplet, but on Ubuntu 24.04 with the same runtime, so these are no
longer worth a manual pass:

- **PHP and its image.** The version the binary reports is the version the
  container runs, all seventeen extensions load, and `servlo production on`
  moves `display_errors`, `expose_php`, `opcache.enable` and
  `opcache.validate_timestamps` together. Checked through the host shims an
  operator types, not through `podman exec`.
- **PHP reaches the database.** A page opening PDO over the podman network,
  read back through HTTP: nginx, the site's own pool, the driver, the container
  network and the engine in one request.
- **A one-click WordPress install, end to end.** Pinned release, checksum,
  hardened extractor, database and scoped user, `wp-config.php` at 0600 with the
  hardening defines, the application's own installer driven over HTTP, a real
  login form and a front page carrying the title the install was given. On a
  domain that resolves nowhere, which is the state a real install runs in.
- **A certificate over HTTP-01, and a renewal that revalidates.** Against Pebble
  rather than Let's Encrypt, but on a genuinely resolvable domain
  (`<ip>.sslip.io`) with the authority connecting back to the machine, plus
  unsecure returning the site to plain HTTP.
- **Boot.** Everything is stopped, proven down, and brought back by starting
  `default.target` alone — which is what the user manager does for a lingering
  user and pulls in only what is enabled. The script names no servlo unit, so a
  unit servlo wrote but never enabled stays down and fails the job.
- **A rebuild from backups.** A second machine with nothing on it: import the
  key, restore the state, reinstall the engine, restore each site, start. Every
  site serves its own page and its settings are intact.
### Still needs a droplet

Take a fresh Ubuntu 24.04 droplet, 2GB or more, and a domain you can point at
it.

```bash
curl -fsSL https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash
```

The installer refuses anything that is not Ubuntu 24.04, and refuses 22.04 with
Podman below 4.5 rather than half-installing. It will print sudo commands for
the privileged steps instead of running them; that is deliberate, and Servlo
never runs sudo itself.

Each line below is something a runner genuinely cannot answer.

**The public internet**
- [ ] A site answers on the droplet's **public** IP, not just loopback. CI only ever reaches the machine it is running on, and a loopback-bound nginx passes that.
- [ ] `servlo doctor` says the port strategy still holds, and still says so after a real reboot of the machine rather than a restart of the user manager.
- [ ] Point a real domain, wait for DNS, press **Get SSL**. A real Let's Encrypt certificate issues over HTTP-01. Pebble answers the same protocol; it does not answer for rate limits, CAA records, or an account that has to be created against the live directory.
- [ ] The button stays disabled while DNS does not resolve here, and says what it is actually seeing.
- [ ] HTTP redirects to HTTPS and the HSTS header is present.
- [ ] Sign in to the panel over HTTPS from another machine, with TOTP on.
- [ ] A Developer account sees only its assigned sites and cannot reach an Admin route, over both HTTP and WebSocket.

**Somebody else's service**
- [ ] Add an S3 destination (DigitalOcean Spaces or Amazon S3) and confirm an upload **actually arrives in the bucket**. This runs rclone in a container and has never talked to a real endpoint.
- [ ] Same for an SFTP destination.
- [ ] The cloud metadata service answering for real, so the Security page links to the right provider's firewall screen. It has only ever seen a stub.
- [ ] `servlo apps install` for Joomla and Grav against their live releases. WordPress is covered; these two hand over at their own setup step, and what is worth checking is that nothing claims a one-click finish they do not deliver.

**A deploy, which nothing has ever run outside a unit test**

No workflow deploys anything. The deploy tests stub every external command with
`exec.Command("true")`, so the whole chain is proven in the sense that the Go
code makes the right decisions and in no other sense: no real `git pull`, no
real dump, no real FPM reload. This list used to say the backup guarantee was
already proven on a real machine, which was this unit test running on a real
runner rather than a deploy happening on one, and that would have taken the
check off the only pass that could make it.

- [ ] A deploy with a migration in its script takes a database snapshot before the pull, and the snapshot is real enough to restore from.
- [ ] A deploy refuses to start at all when it cannot tell which database to back up, rather than pulling and running the migration anyway.
- [ ] A deploy script that fails leaves visitors on the last version that worked. Production OPcache is what makes that true, so it is not observable anywhere the container is not real.
- [ ] Redeploy previous commit puts the code back and says plainly that it did not put the data back.
- [ ] Both ways in: the panel's Deploy button and the deploy webhook.

**Time, and things containers hide**
- [ ] Log rotation against an application holding its log file open across requests. Rotation renames rather than copies, and that is the case it is chosen for.
- [ ] A WordPress deploy leaves `wp-content/uploads` and `wp-content/plugins` untouched. Losing a client's media is the failure this exists to prevent.
- [ ] A certificate actually renewing on its own. The watcher sweeps twice a day and on start, and the sweep is unit-tested, but nothing has yet watched a real certificate cross into its reissue window unattended.

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
it. Tagging `v0.1.0` runs `release.yml`. Until that tag exists, `install.sh`
resolves `releases/latest` and finds nothing, so the documented one-line install
cannot work for anybody but you.

**The organisation, and going public.** Both are done: the repository is public
and lives under `ServloOfficial`, which is what switched the store update path
on and what pays for the runners. What is left of this item is the release
above.

---

## 5. Deliberately not done

From PRD §10, deferred on purpose. Do not let anyone talk you into these before
real use asks for them: atomic releases with true rollback, per-site Linux user
isolation, Prometheus metrics, managing more than one server from one panel.

Also settled and not up for revisiting: every site runs as the same Linux user
(PRD §6), SSH password authentication is never disabled, and there is no mail
server, only SMTP settings.
