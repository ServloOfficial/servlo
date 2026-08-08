# Deploy

Every site has its own deploy script. Servlo seeds it from the framework's [deploy profile](framework-definitions.md#deploy-profile) the first time you open it and never touches it again, because what a particular application needs at deploy time is not knowable from its framework.

## What a deploy does

Press **Deploy** on the site, or POST to `/api/sites/{domain}/deploy`. Four phases, in this order, each one refusing to start if the one before it did not finish:

1. **Database backup**, if the script runs a migration. Named `predeploy-<site>-<timestamp>`.
2. **`git pull --ff-only`**.
3. **The deploy script**, with the site as its working directory.
4. **A graceful PHP-FPM reload**, which is what makes the new code live.

Output streams into the panel as it happens, both stdout and stderr, with each phase announced before it runs. A deploy takes minutes and a spinner that eventually says "failed" is exactly what this replaces.

The order is the design, and each part of it is deliberate:

**The backup comes first**, before the pull. A pull that fails halfway still leaves a database somebody may need, and taking it first means the snapshot is of the state the site was actually in.

**If the backup fails, nothing is deployed.** A guarantee that only holds when nothing goes wrong is not one, and this is the case it exists for. If servlo cannot tell which database to back up, that counts as a failed backup: set `DB_HOST` and `DB_DATABASE` in the site's `.env`, or take the migration out of the script.

**The pull is fast-forward only.** A deploy that merges is a deploy that can produce a commit nobody wrote and nobody reviewed, on a server, unattended.

**The script stops at its first failing command.** Otherwise a `composer install` that could not reach the network is followed by a migration against half-installed code, and the deploy reports success.

**A failed script does not reload PHP-FPM**, and that is the safe direction rather than an oversight. Production OPcache runs with `validate_timestamps=0`, so PHP keeps serving the bytecode it already has: withholding the reload leaves visitors on the last version that worked rather than on the half-deployed one now on disk. The failure says so explicitly, along with the commit the pull had already reached.

**The reload is graceful.** `SIGUSR2` to the FPM master, which starts new workers and lets the running ones finish. A deploy that dropped every in-flight request on every site sharing the container would be worse than the problem it solves.

A deploy takes the same per-site lock as the command runner and the doctor's fixes, so it cannot run at the same time as either. Two processes writing the same `vendor` directory is not a race worth having.

An empty script is a valid deploy: the pull is the deploy, which is the ordinary case on WordPress.

## Where the script lives

`~/.local/share/servlo/deploy-scripts/<site>.sh`, not in the site directory.

That is deliberate. A deploy runs `git pull` first, so a script inside the working tree is a file the pull can rewrite while it is the thing running the deploy. Keeping it outside also means a fresh clone of the same repository somewhere else does not inherit it, and it is not something anyone can commit by mistake.

The script runs as the servlo user with the site directory as its working directory. Its output streams into the panel, and a non-zero exit fails the deploy.

## What it starts as

A header explaining all of that, followed by the framework's template. For Laravel that is the usual production sequence: a `--no-dev` composer install with an optimised autoloader, an asset build, migrations, the config, route and view caches, and a queue restart. For WordPress it is empty, because on most WordPress installs the pull is the deploy and a script that assumed otherwise would fight the client's admin screen.

A framework with no profile, and a site with no framework, get the header and nothing else.

## Editing it

Saving keeps a timestamped backup of what it replaced, in a `bkp/` directory beside the script, and any backup can be restored. Resetting deletes the script so the site goes back to its framework's template; the backups survive that too.

Nothing validates the contents beyond refusing a NUL byte. It is a shell script you wrote to run on your own server, and servlo guessing at which commands are reasonable would be both wrong and impossible to get right.

## Migrations and the pre-deploy backup

Servlo reads the saved script to decide whether a deploy is schema-changing, matching it against the migration command the framework declares. That match is what triggers the automatic database backup before the deploy.

It reads the script that will actually run, not the framework's template, so taking the migration out means no backup and adding one the template never had means there is. Blank and commented-out lines do not count: a migration somebody commented out would otherwise snapshot the database on every deploy from then on.

A framework that declares no migration command has no schema-changing deploys, and nothing on it is ever backed up on that basis.
