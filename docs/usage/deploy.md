# Deploy

Every site has its own deploy script. Servlo seeds it from the framework's [deploy profile](framework-definitions.md#deploy-profile) the first time you open it and never touches it again, because what a particular application needs at deploy time is not knowable from its framework.

## What a deploy does

Press **Deploy** on the site's Deploy tab, or POST to `/api/sites/{domain}/deploy`. Five phases, in this order, each one refusing to start if the one before it did not finish:

1. **Database backup**, if the script runs a migration. Named `predeploy-<site>-<timestamp>`.
2. **`git pull --ff-only`**.
3. **Putting back** anything the pull removed that the site's [exclude list](#paths-a-deploy-must-not-remove) protects.
4. **The deploy script**, with the site as its working directory.
5. **A graceful PHP-FPM reload**, which is what makes the new code live.

Output streams into the panel as it happens, both stdout and stderr, with each phase announced before it runs. A deploy takes minutes and a spinner that eventually says "failed" is exactly what this replaces.

The order is the design, and each part of it is deliberate:

**The backup comes first**, before the pull. A pull that fails halfway still leaves a database somebody may need, and taking it first means the snapshot is of the state the site was actually in.

**If the backup fails, nothing is deployed.** A guarantee that only holds when nothing goes wrong is not one, and this is the case it exists for. If servlo cannot tell which database to back up, that counts as a failed backup: set `DB_HOST` and `DB_DATABASE` in the site's `.env`, or take the migration out of the script.

**A managed database is backed up too.** A database servlo runs is dumped inside its own container; one it does not, a DigitalOcean Managed database or any other, is dumped over the connection the site is on and filed in the same snapshot store. Either way the deploy stops if the dump does. Restoring a managed database's snapshot is not wired into `servlo db:restore` yet, so that one is a `gunzip` and the engine's own client for now.

**The pull is fast-forward only.** A deploy that merges is a deploy that can produce a commit nobody wrote and nobody reviewed, on a server, unattended.

**The script stops at its first failing command.** Otherwise a `composer install` that could not reach the network is followed by a migration against half-installed code, and the deploy reports success.

**A failed script does not reload PHP-FPM**, and that is the safe direction rather than an oversight. Production OPcache runs with `validate_timestamps=0`, so PHP keeps serving the bytecode it already has: withholding the reload leaves visitors on the last version that worked rather than on the half-deployed one now on disk. The failure says so explicitly, along with the commit the pull had already reached.

Know what that covers. It keeps live what the cache already holds, and a file the cache has never seen is compiled off disk on the next request however new it is. A route nobody has hit since the last successful deploy will serve the half-deployed code, and so will every route on a site quiet enough that the cache was still empty. A deploy is a pull in place and the tree is not rolled back, so this is protection rather than a guarantee: on a busy site it holds, on a quiet one it may not. Fix forward, or use [going back a deploy](#going-back-a-deploy).

**The reload is graceful.** `SIGUSR2` to the FPM master, which starts new workers and lets the running ones finish. A deploy that dropped every in-flight request on every site sharing the container would be worse than the problem it solves.

**Files are put back before the script, not after it.** A script that rebuilds a cache or runs a plugin update should see the tree the site actually has, not one briefly missing the client's plugins.

A deploy takes the same per-site lock as the command runner and the doctor's fixes, so it cannot run at the same time as either. Two processes writing the same `vendor` directory is not a race worth having.

An empty script is a valid deploy: the pull is the deploy, which is the ordinary case on WordPress.

## Where the script lives

`~/.local/share/servlo/deploy-scripts/<site>.sh`, not in the site directory.

That is deliberate. A deploy runs `git pull` first, so a script inside the working tree is a file the pull can rewrite while it is the thing running the deploy. Keeping it outside also means a fresh clone of the same repository somewhere else does not inherit it, and it is not something anyone can commit by mistake.

The script runs as the servlo user with the site directory as its working directory. Its output streams into the panel, and a non-zero exit fails the deploy.

## What it starts as

A header explaining all of that, followed by the framework's template. For Laravel that is the usual production sequence: a `--no-dev` composer install with an optimised autoloader, an asset build, migrations, the config, route and view caches, and a queue restart. For WordPress it is empty, because on most WordPress installs the pull is the deploy and a script that assumed otherwise would fight the client's admin screen.

A framework with no profile, and a site with no framework, get the header and nothing else.

## Editing the script

Edit it on the site's Deploy tab. Saving keeps a timestamped backup of what it replaced, in a `bkp/` directory beside the script, and any backup can be restored. Resetting deletes the script so the site goes back to its framework's template; the backups survive that too.

Nothing validates the contents beyond refusing a NUL byte. It is a shell script you wrote to run on your own server, and servlo guessing at which commands are reasonable would be both wrong and impossible to get right.

It runs under `/bin/sh`, which on Ubuntu is dash. Servlo runs the body rather than the file, so the shebang on the first line is a label and changing it changes nothing: a `[[ ]]`, an array or a `set -o pipefail` below it fails with a complaint about the option rather than about the shell. Keep it to POSIX sh, or call bash from inside the script.

## Migrations and the pre-deploy backup

Servlo reads the saved script to decide whether a deploy is schema-changing, matching it against the migration command the framework declares. That match is what triggers the automatic database backup before the deploy.

It reads the script that will actually run, not the framework's template, so taking the migration out means no backup and adding one the template never had means there is. Blank and commented-out lines do not count: a migration somebody commented out would otherwise snapshot the database on every deploy from then on.

What counts as a migration is the framework's own to say, and every framework definition says it, as the distinctive part of the command rather than one way of invoking it. Laravel and Statamic recognise `artisan migrate`, Symfony `doctrine:migrations:migrate` and its `d:m:m` shorthand, CakePHP `migrations migrate`, CodeIgniter `spark migrate`, Tempest `tempest migrate:`, Magento `setup:upgrade`, and Drupal both `updatedb` and its `updb` alias. Grav, Joomla and WordPress say plainly that they have none: their schema changes come from a plugin or a core update, which servlo does not drive, so no deploy on them is treated as schema-changing.

It is a substring match, which decides where it errs. `spark migrate` also matches `php spark migrate:rollback`, and a rollback wants the same snapshot behind it, so the extra backup is the right answer. That is the direction to err in: an unnecessary dump costs a little disk and a little time, and a missing one costs the database.

The other direction is the one to know about: a migration servlo does not recognise is a deploy with no backup and no warning, because from servlo's side there was nothing to warn about. The markers come from the site's framework definition, and every binary ships every definition it was built with, so a site resolves one even when its version cannot be read and nothing can be fetched. The markers carry no invocation for that reason, so `./artisan migrate`, `php artisan migrate` and `$PHP artisan migrate` all count, and where a command has a documented alias the definition names both. What is left is a script that migrates through a wrapper of your own, whose name servlo cannot guess. If yours does, add the framework's own command to the script beside it, or take a snapshot before you deploy.

## Which Node the build uses

The site's, not the daemon's. Servlo runs the whole deploy script under the Node version the site is pinned to, so `npm ci && npm run build` in a deploy script produces a bundle from the toolchain that site chose.

The whole script goes under that version rather than each command, because a script is several commands and the second one needs the same Node as the first.

If a site pins a version that is not installed, the deploy stops and says so rather than quietly building with a different one. It does not install it for you: a deploy that paused to download a toolchain would be minutes of surprise in the middle of an operation somebody is watching. Run `servlo node:install <version>` and deploy again.

A site with no Node pin, or a machine with no Node at all, runs the script as an ordinary shell script. Most WordPress sites never run a build and should not need a Node manager to exist.

## Asset builds and memory

`npm run build` is the hungriest thing servlo ever runs, and on a small droplet it is the thing most likely to exhaust the machine. When that happens the kernel's OOM killer picks a victim by its own arithmetic, and that victim is routinely MySQL, because MySQL is the biggest resident process on the box. One site's oversized build takes every other site's database with it, and what the operator sees is a database outage rather than a failed deploy.

So the deploy script runs inside its own systemd scope with a memory ceiling. The build is the only thing in that cgroup, and when it hits the ceiling the cgroup's OOM killer stops the build. One deploy fails, loudly, saying it ran out of memory and what to do about it. Nothing else on the machine notices.

The ceiling is half the machine's memory, floored at 512MB and capped at 4GB. Half leaves the other half for the databases, the PHP pools and nginx, which is what has to survive. The floor is there because a build capped below 512MB fails on almost any real front end, and refusing to build is not an improvement on failing under load. The ceiling is there because past a few gigabytes a build is not hungry, it is broken, and a large droplet should not hide that for longer.

Swap is not available to the build. With swap it would not fail at the ceiling, it would start paging, and a droplet thrashing for twenty minutes is worse for every site on it than one deploy failing in two. The [swap file](../getting-started/installation.md) servlo sets up at install exists so the system survives pressure, not so one build can ignore its limit.

On a machine with no systemd user session the build runs unconfined. A panel that refused to build assets because it could not confine them would trade a rare failure for a certain one.

### The warning before the build

On a small machine, a deploy that compiles assets says so before it starts, naming the ceiling. Before rather than after, because after is a failed deploy and the moment to do something about it has passed.

It fires only when both halves are true: this machine is at the floor of what servlo will allocate, and this deploy actually builds something. A line on every deploy is a line nobody reads by the third one. A commented-out build does not count.


## Deploy history

Every deploy writes down what it did, and the Deploy tab lists the recent ones: the commit and its subject, who wrote it, who triggered the deploy from the panel, how long it took, whether it worked, and the reason if it did not.

Failures are in the list. A history that remembered only the deploys that worked could not answer what happened at 3am, which is the question it gets asked.

The author and subject are read while the commit is still checked out and stored with the entry, because by the time anyone reads the history the tree has usually moved on. "No signed-in user" means the deploy did not come from a panel session, which is how a scheduled or webhook deploy will appear.

It lives at `~/.local/share/servlo/deploy-history/<site>.jsonl`, one JSON object per line, appended and never rewritten in place, mode 0600. A site keeps its last 200 deploys; the file is trimmed in batches, through a temporary file and a rename, so a crash part way through leaves the previous history rather than a truncated one. A line that will not parse is skipped rather than failing the read, so one half-finished write cannot take the rest of the log with it.

## Going back a deploy

A deploy that made things worse has one button: **Go back a deploy**, on the Deploy tab, or POST to `/api/sites/{domain}/redeploy`. It checks the site out at the commit it was running before the last deploy and runs the deploy script again.

**Code only. Database migrations are not undone.** The schema stays as the last deploy's migrations left it, and the older code has to run against it. That is stated on the button and in the deploy output, because an operator who believes the data went back too will make the next decision on a false premise. To put the data back, restore that deploy's snapshot yourself, deliberately.

There is no automatic reverse migration and there will not be one. Reversing a migration means running your own `down()` against production data on the say-so of a panel button, and the frameworks that offer it do not promise it is lossless.

### Which commit it goes back to

The one the site was standing on before the last successful deploy, **not** `HEAD~1`. A deploy that pulled five commits moved the site five commits, and `HEAD~1` is a commit nobody ever ran. Servlo knows the difference because every deploy writes down where it started, in `~/.local/share/servlo/deploy-history/<site>.jsonl`.

That has two consequences worth knowing. A site whose first deploy from this panel has not happened yet has nothing recorded and the button is not offered. And the target comes from that record rather than from the request, so the panel cannot ask to go back to an arbitrary commit.

A deploy that failed, or one that pulled nothing, is skipped when looking for the target: neither moved the site, so going back to where they started would land on the commit already running.

### The rest of it is an ordinary deploy

It takes the same per-site lock, honours the same [exclude list](#paths-a-deploy-must-not-remove), runs the same script, and reloads PHP the same way. The exclude list matters more here than on a pull: a checkout removes tracked files the older commit does not have, which is exactly how a plugin installed after that commit would disappear.

It never pulls. The operator is going backwards, and going to the network first is how a rollback ends up back on the commit it was escaping.

As with a forward deploy, a failed script means no reload, so visitors stay on the version that was working rather than on a half-prepared rollback, with the same limit: what the cache never held is read off disk.


## Paths a deploy must not remove

Some directories belong to the application rather than to the repository: everything a client has uploaded, every plugin they installed through an admin screen. A deploy that removed one would destroy the site, and the operator would find out from the client.

Each site has a list of those paths. It starts as its framework's, declared in the [deploy profile](framework-definitions.md#deploy-profile), and a WordPress site gets `wp-content/uploads` and `wp-content/plugins` without anybody configuring it. Edit it on the Deploy tab, one path per line relative to the site directory, or through `/api/sites/{domain}/deploy-exclude`.

### What it is actually protecting against

On most installs, nothing, and that is worth knowing before you go looking for the list to do more than it does.

`wp-content/uploads` and `wp-content/plugins` are normally gitignored, so git does not track them, and a file git does not track is one a pull cannot touch. The list has nothing to do on those sites.

The sites that lose a client's data are the ones whose repository committed those directories, and there git behaves in two different ways:

- A file you changed **locally**, git refuses to overwrite. The pull fails, loudly, naming the file. Nothing is lost and nothing needs protecting.
- A file that was deleted **upstream**, git deletes here, silently, because from its point of view nothing local was at risk.

That second case is the whole gap. A developer tidying a repository from a checkout that never had the client's plugins removes them from every site running it. So the list protects against deletion: after the pull, any file under one of these paths that the update removed is written back at the content it had before.

Restoring only deletions is also what makes the repair clean. The file is gone from the new commit, so writing the old content back leaves it **untracked**, and every later pull is as clean as it was before. Restoring over a file git still tracks would leave the tree permanently dirty and wedge the next deploy, which is a worse failure than the one being prevented because it arrives later.

If a path could not be put back, the deploy fails and says so. Carrying on would report success having lost exactly what the list exists to keep.

### Changing the list

Saving replaces the list for this site, and the site stops following its framework. **Go back to the framework's list** undoes that, which is a different thing from saving an empty list: an empty list protects nothing, and following the framework protects whatever the definition declares, including a definition updated after the site was created. The form says which of the two the site is on.

Paths are relative to the site directory and one that climbs out of it is refused. They match whole path segments, so `wp-content/uploads` does not reach into `wp-content/uploads-old`.

## Deploy on push

A site can deploy itself whenever somebody pushes. It is **off until you turn it on**, per site, on the Deploy tab.

Turning it on mints two things. The **payload URL** ends in a random public identifier and is what you paste into your repository's webhook settings, content type `application/json`. The **secret** keys the signature, and servlo shows it once, on the response that creates it, and never again. Lose it and you generate a new one, which keeps the URL and invalidates every old signature at the same time.

Set the **branch that deploys**. A push to anything else is ignored. Leaving it empty deploys every branch, which is a real choice and rarely the right one on a site serving customers.

### What it checks before it deploys

A request that fails any of these deploys nothing:

- **The signature.** `X-Hub-Signature-256`, HMAC-SHA256 over the raw body, keyed by that site's secret and compared in constant time. This is the whole authentication: nothing else about the request is evidence. The source address belongs to a host with a large and changing range, and a token in a header is a token in every proxy log along the way.
- **The branch.** From `refs/heads/...` in the payload. A tag push and a branch deletion carry no branch and never deploy.
- **The delivery.** `X-GitHub-Delivery` identifies one delivery attempt, and servlo remembers the recent ones per site so a retry, or a captured body sent again, runs once. A request with no delivery header is refused rather than waved through: without it a replay cannot be told from a new push, and a signed body stays valid forever.

The body is capped at a megabyte and read no further. This is the only route on the panel that takes bytes from the internet with no session behind them.

### What it answers

`200` when it deployed, and also when it deliberately did not: a push to another branch, or a delivery already handled. That is on purpose. A git host retries a `5xx` and eventually disables a hook that keeps failing, and neither of those is a failure.

`401` for a bad signature, `404` for an endpoint that does not exist or has been turned off, and `409` while the site is busy with another deploy, which is worth the host retrying.

A webhook deploy is an ordinary deploy: same pre-deploy database backup, same [exclude list](#paths-a-deploy-must-not-remove), same script, same graceful reload. It appears in the [deploy history](#deploy-history) like any other, attributed to no signed-in user, because there wasn't one.

### Where the credentials live

`~/.config/servlo/deploy-webhooks.json`, mode 0600, and nothing else is in it. Not the site registry: that file is world readable by design, which is right for ports and paths and wrong for a token that deploys code to your server.
