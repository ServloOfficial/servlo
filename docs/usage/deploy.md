# Deploy

Every site has its own deploy script. Servlo seeds it from the framework's [deploy profile](framework-definitions.md#deploy-profile) the first time you open it and never touches it again, because what a particular application needs at deploy time is not knowable from its framework.

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
