# The resilience jobs

Two things Phase 4 claims cannot be checked by a unit test, because both are about what survives losing the machine. They run in CI, on a real Ubuntu 24.04 runner, against real podman, real systemd user units and real HTTP.

They live in `.github/workflows/resilience.yml` and take minutes rather than seconds, which is why they are not in `ci.yml`. When one goes red, the thing that broke is a different kind of thing from a failing unit test.

## Everything comes back on its own

Builds this checkout, installs it, creates a site, proves it serves. Then it stops every servlo unit, proves the site has actually stopped serving, and starts `default.target`.

That last step is the point. Starting `default.target` is exactly what the user manager does when a lingering user's session begins at boot: it pulls in every unit that is `WantedBy=default.target` and nothing else. Nothing in the script names a servlo unit to start, deliberately, because a script that started them by name would pass whether or not they were enabled and would be testing itself.

Then it asserts the site serves again, with nobody having run `servlo start`, and separately that every unit servlo installed with an `[Install]` section is enabled. A unit that is running but not enabled looks perfectly healthy in every dashboard servlo has, and is gone the first time the droplet restarts, months later and during an upgrade nobody connects it to.

Be straight about what this does not do. It does not reboot a kernel, so it does not cover a container image that fails to start from a cold page cache, or a tmpfs a reboot empties. Those belong to the droplet pass. What it does cover is the enablement, which is where the failure has always actually been.

Autostart is switched on explicitly in the job. Autostart off is a legitimate choice and it strips the `[Install]` section from the quadlets, so leaving it to the default would fail the job for a reason that is not a bug.

## A fresh machine plus the backups is the old machine

Two jobs and an artifact between them, which is the closest CI has to a second droplet.

The first builds a server: two sites, a database, a backup schedule on one of them. It proves both serve, then takes per-site backups and `servlo backup state`, and uploads the archives.

The second runs on a runner that has never seen the first. It installs servlo and nothing else, downloads the archives, and does the rebuild in the order the docs give:

1. `servlo backup key import` — without it nothing opens, because servlo has generated a key of its own on the new machine and it opens nothing the old one wrote
2. `servlo restore … --state` — the registry, the connections, every per-site setting
3. the database engine, added only now
4. `servlo restore …` for each site
5. `servlo start`

Step three is the one with a trap in it. The install on this runner asks for no database at all, and the engine is added after the state restore rather than before. servlo's service password lives in the config directory a state archive carries, and MySQL bakes the password it is handed into its data directory the first time it starts. Create the engine first and it holds the new machine's password while the restored config holds the old one, so every dump load afterwards is refused with an access-denied that says nothing about backups. This is the first thing CI ever caught here.

Then it asserts every site serves its own page, and that the backup schedule set on the first machine came back with it.

The backup key travels between the jobs as a job output. It is generated for that run, it encrypts nothing but the two fixture sites, and a real key never leaves the server it was generated on.

## What they share

Four small scripts under `.github/scripts/`, so the two jobs cannot drift apart on what "installed" or "serving" means:

| Script | What it does |
|---|---|
| `prepare-runtime.sh` | Linger, the user bus, podman, and the unprivileged-port sysctl |
| `install-servlo.sh` | Build this checkout and run the one-time setup unattended |
| `make-site.sh` | A directory with one PHP file in it, linked to a domain |
| `expect-serving.sh` | Ask the site over HTTP and check the body, not just the status |

`expect-serving.sh` uses `curl --resolve` rather than writing a hosts entry, because servlo never touches a host resolver and its CI does not either. It checks the body as well as the status, since nginx's default vhost will happily answer 200 for a site that is not there.

The sysctl in `prepare-runtime.sh` is run by the workflow with `sudo`, never by servlo. That division is the rule rather than a CI shortcut: servlo prints the command that needs privilege and a person runs it. In CI the workflow is the person.
