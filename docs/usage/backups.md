# Backups

A backup is one encrypted archive holding a site's files and a dump of its database.

## Commands

| Command | Description |
|---|---|
| `servlo backup [site]` | Take a backup of the site |
| `servlo backup list [site]` | The backups on this server, newest first |
| `servlo backup key` | Where this server's backup key is |
| `servlo backup schedule [site] [when]` | Back the site up on a schedule, or show the current one |
| `servlo backup schedule <site> --off` | Stop backing it up on a schedule |
| `servlo backup verify <archive>` | Restore it into a scratch database and check what came back |
| `servlo backup state` | Back up servlo's own configuration and the site registry |
| `servlo backup key import <key>` | Bring a backup key over from another server |
| `servlo backup destination` | Where finished archives are copied to |
| `servlo backup destination add <name>` | Add an S3 or SFTP destination |
| `servlo backup destination test [name]` | Check a destination can be reached |
| `servlo restore <archive>` | Put an archive back |
| `servlo restore <archive> --state` | Put the server's own configuration back |

With no site named, `servlo backup` backs up the site the shell is standing in.

## The key

Every backup this server writes is encrypted, and one key opens all of them. It lives at `~/.config/servlo/backup.key`, mode `0600`, and it is generated the first time anything needs it.

**Copy it somewhere off this server now.** A backup you cannot open is not a backup, and the moment you will discover that is the moment the server it was on is gone. Servlo will not rotate the key on its own, because rotating it would leave every archive already written unopenable.

Backups are encrypted because of where they end up: object storage, or another machine. Encrypting before the archive leaves this process is what makes it safe to store somewhere you do not control. The key never travels with an archive.

A key file that is the wrong length is refused rather than replaced. Replacing it is how the existing backups would quietly stop being recoverable, so servlo says what is wrong and leaves the file alone.

## On a schedule

Nothing is scheduled until you ask for it. A backup timer nobody switched on is a disk filling up on a server whose owner does not know it is happening, so the panel and the CLI both say plainly which sites have one and which do not.

```bash
servlo backup schedule acme daily
servlo backup schedule acme "30 3 * * *"
servlo backup schedule acme "*-*-* 03:30:00"
```

All three work. It is the same parser scheduled commands use, because an operator who has typed a crontab line into the cron tab should not find that the backup field wants something else.

The timer is `Persistent`, so a droplet that was off overnight takes the backup it missed when it comes back rather than skipping the day silently. It carries a small randomised delay so every site on a server does not start dumping at the same second.

The unit runs servlo by absolute path. A unit that resolved `servlo` from `PATH` would stop working the first time `PATH` changed, and the way that failure shows up is a backup that quietly stopped happening months ago.

## Retention

Every backup ends by thinning the older ones for that site, on the ordinary grandfather-father-son rule: keep the newest of each of the last N days, then of each of the last N weeks, then of each of the last N months.

```bash
servlo backup schedule acme daily --daily 7 --weekly 4 --monthly 3
```

That is also the default for a site that has not said otherwise. Keeping everything by default is how a server fills up quietly, and keeping nothing by default would be worse.

The sweep runs as part of the backup rather than as a second timer, because a retention timer nobody armed is the same disk filling up. It only ever touches archives matching that one site, and only files that are actually archives: a checksum sidecar, a note, a partial file from an interrupted run and a decompressed copy are all left alone.

A sweep that fails is reported as a warning, not as a failed backup. The archive is already on disk, and saying otherwise would have you re-running something that worked.

## Verifying a backup

```bash
servlo backup verify acme-20260809-030000
```

This is the part worth having. It opens the archive, restores its dump into a database of its own, counts what arrived, and drops the scratch database again. The site's own database is never touched.

A backup that has never been restored is not a backup. Most panels ship a green tick that means a file was written, which says nothing about whether it can be put back, and the day you find out is the day it matters.

The check fails if the dump will not load, and it also fails if the dump loads without error and leaves **no tables behind**. That second case is the one a naive check misses: nothing errored, so nothing looked wrong.

What it counts is tables, not a checksum against the live database. The live one has moved on since the backup was taken, so comparing against it would fail every time and teach you to ignore the result.

The scratch database is named `servlo_verify_<site>_<random>` and is dropped on every path out, including a failed load. If you ever find one left behind by a killed process, it is safe to drop: the name says what it is, and the random tail is there so two verifies running at once cannot drop each other's.

An archive holding no database reports that plainly rather than passing quietly. Opening it still proved something: it decrypted, and it was complete.

### On a schedule

```bash
servlo backup schedule acme daily --verify "Sun *-*-* 04:00:00"
```

That arms a second timer which checks the newest archive every week. It is its own unit pair, so switching one off leaves the other alone and `systemctl --user list-timers` shows which of the two is failing.

Weekly rather than nightly, and deliberately not folded into the backup itself. A verify restores the whole database into a scratch copy, which on a large site is real work, and doing it after every backup would double the nightly cost to answer a question whose answer changes rarely.

`servlo backup schedule acme` says whether a check is armed, and says so plainly when one is not.

## Sending archives somewhere else

A backup that only exists on the machine it is a backup of is not a backup. Archives are copied to every configured destination as soon as they are written.

```bash
servlo backup destination add spaces --kind s3 \
  --bucket acme-backups --prefix servers/lon1 \
  --endpoint https://fra1.digitaloceanspaces.com --region fra1 \
  --access-key DO00... --secret-key ...

servlo backup destination add offsite --kind sftp \
  --host backups.example.net --user servlo \
  --path /srv/backups/lon1 --key-file ~/.ssh/backup_ed25519
```

S3-compatible covers DigitalOcean Spaces and Amazon S3 with one driver. The prefix is what lets one bucket hold several servers without them overwriting each other.

Neither protocol is spoken by servlo itself. rclone runs in a container on the servlo network, the same way the database clients do, because it already speaks both correctly and the alternative is hand-rolling request signing whose first failure would be a backup that silently never arrived.

The credentials travel in the environment, never in an argument list, so nothing running as the same user can read them out of `ps`. They are stored at `~/.config/servlo/backup-destinations.yaml`, mode `0600`.

**A destination failing does not fail the backup.** The archive is on this server and usable; what failed is the copy going elsewhere, and reporting otherwise would have you re-running a backup that worked. Every destination is attempted even after one fails, because two exist precisely so that one being unreachable is survivable.

`servlo backup destination test` lists what is actually at each one, which is the check worth running after adding it rather than waiting for 03:30.

Removing a destination leaves the archives already there alone. Deleting somebody's offsite copies as a side effect of editing a setting is never what was meant.

## The server's own state

```bash
servlo backup state
```

A site's archive holds that site. Restoring one onto a fresh droplet gives you the files and the database on a server that has no idea what a site is: no registry, no connections, no schedules, no settings.

The panel has it too, on **System, Servlo**, as the Server state card: what exists, when each was taken, and a button that takes another.

`servlo backup state` is the other half. It carries everything servlo knows that is not a site's files or data: the site registry, the connections, the per-site database accounts, the SMTP settings, the provider certificates and every per-site setting.

**The backup key is deliberately not in it.** It is what opens the archive, so putting it inside would be locking the door and taping the key to the front.

The two kinds refuse each other. A state archive handed to `servlo restore` says so and names the command to use, and a site archive handed to `--state` does the same, because the two unpack to completely different places and getting them the wrong way round would empty a server's configuration over a site directory.

### Rebuilding onto a fresh droplet

The order matters, and the first step is the one that is easy to miss:

```bash
servlo backup key import <the key from the old server>
servlo restore servlo-state-20260809-030000 --state
servlo restore acme-20260809-030000
```

Without the key first, nothing opens: servlo will have generated a key of its own on the new machine, which opens nothing that the old one wrote. It says so when that happens rather than leaving "wrong key" to be puzzled over.

Two things a restore cannot give back, both said plainly at the end of one:

- **Certificates are reissued, not restored.** Let's Encrypt binds them to the domain, and DNS has to point at the new server before it will issue. Point DNS, then run `servlo secure` per site.
- **A managed database needs the new address** added to the provider's trusted sources, or every site on it will fail to connect from a server the provider has never seen.

## Restoring

```bash
servlo restore smokesite-20260809-113816
servlo restore /path/to/archive.servlobak --into /tmp/inspect
servlo restore acme-20260809-030000 --files-only
```

The name printed by `servlo backup list` is enough; a full path works too.

What the archive says about itself is read before anything is written, so a restore into the wrong place is refused rather than discovered afterwards. Then it asks you to **type the site's name**. A y/n prompt is muscle memory by the time anyone reaches a restore, and this one overwrites a live site.

A restore replaces what the archive holds and leaves everything else where it is. It is not a way to undo a file that was added since the backup was taken.

`--into` restores somewhere else, which is how you look inside an archive without touching the site. A restore into a plain directory never touches a database. `--files-only` restores the files and leaves the database alone.

The dump is piped from the archive straight into the engine's client, so a database larger than the droplet's memory never lands anywhere in between.

### What a restore refuses

An archive is a file from somewhere else. It may have come off storage nobody here controls, or off the machine being rebuilt precisely because something went wrong with it, so nothing in it is taken on trust.

Every entry is resolved against the directory it is going into and refused if it lands outside, checked on the resolved path rather than by looking for `..` in the name: `a/../../b`, a leading slash and `../site-evil/x` all escape and none of them contains a `..` component the naive check would find. Symlinks are not restored at all, since a link is a way to write through it on the next entry. An entry that is not the manifest, the dump, or under `files/` is refused outright rather than skipped, because quietly restoring part of a malformed archive is worse than refusing all of it.

An archive written by a newer servlo is refused before anything is written, with a message saying to update.

## What is in an archive

Three things: the site's files under `files/`, the database as `database.sql`, and a `manifest.json` saying which site, when, which framework, which database and what was left out.

The manifest is written last. That is deliberate: an archive that has one is an archive that finished, so a backup interrupted halfway is recognisable as incomplete instead of discovered to be short during a restore.

The archive is written under a temporary name and moved into place only once it is complete, and two backups taken in the same second get different names. Both guard the same thing, from different directions: a half-written file that kept its final name would be counted by a retention sweep and picked up by a restore, and a name collision would lose yesterday's good archive to today's.

Archives land in `~/.local/share/servlo/site-backups/`, mode `0600`, because one holds an entire site including its `.env`.

## What is left out

The framework's definition says, under a `backup` block:

```yaml
backup:
  exclude:
    - vendor
    - node_modules
    - storage/framework/cache
    - bootstrap/cache
```

Only what a deploy puts back belongs on that list. `composer install` rebuilds `vendor`, `npm ci` rebuilds `node_modules`, and a compiled cache is written again on the next request. Carrying them would make every archive an order of magnitude larger and restore nothing.

What is deliberately **not** on any framework's list is anything the application writes and nothing rebuilds: Laravel's `storage/app`, WordPress's `wp-content/uploads` and `wp-content/plugins`, Drupal's `sites/default/files`, Magento's `pub/media`. A restore is exactly as good as what the archive carried.

Note this is the opposite of the `deploy.exclude` list beside it. A deploy exclude is something too precious to remove; a backup exclude is something cheap enough to rebuild. They are separate lists and a path belongs on at most one.

A site can override the framework's list with its own. Clearing it to empty is a decision that survives: an empty list means this site's backup carries everything, and a site that never set one keeps following its framework's definition, including one updated after the site was created.

A symlink pointing out of the site is recorded but not followed. Copying what it points at would pull the rest of the server into one site's archive, and writing it back out on restore would land outside the site.

## Databases, local and managed

Both are backed up, and the difference is where the dump runs.

For a database servlo hosts, the dump runs inside that engine's container, which is where the engine and its client already are. For a managed database there is no container here, so the engine's client runs on the servlo network aimed at the provider's host and port, with the TLS flags that engine's definition spells and the provider's CA certificate mounted in.

Both statements are the engine's own, declared in its definition under the `databases` entity as `export` and `remote_export`. Adding an engine adds its statements and needs no servlo release.

The administrator's password reaches the client through the environment, forwarded into the container by name. It is never spelled in an argument list, where anything running as the same user could read it out of `ps`.

**A dump that fails fails the backup.** An archive holding the files and a truncated dump is worse than no archive, because it looks like a backup until the day it is needed.

A site with no database servlo can name, one on SQLite for instance, still backs up. The manifest records that there is no database in there, and the command says so, so nothing later mistakes it for a backup whose data went missing.
