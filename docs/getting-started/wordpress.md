# WordPress

From a fresh server to a WordPress site serving HTTPS on its own domain.

::: info Prerequisites
`servlo install` has run on this server. If not, see [Installation](installation.md).
:::

## The short version

```bash
servlo apps install wordpress myblog.example.com --admin-email you@example.com
servlo secure myblog.example.com
```

That is the whole install. The first command downloads a pinned WordPress
release, verifies it against a recorded sha256, extracts it, creates a database
and a least-privilege user for it, writes `wp-config.php` with the eight keys
WordPress wants, generates an administrator account, and registers the site.
The admin password is **printed once and never written to a log**, so copy it
before you clear the terminal.

The second issues a Let's Encrypt certificate, rewrites the vhost to 443, adds
the redirect and updates the site URL. It refuses until `myblog.example.com`
actually resolves to this server — see [HTTPS / TLS](/features/https).

Options worth knowing:

| Flag | Default |
|---|---|
| `--admin-user` | `admin` |
| `--admin-email` | asked for; WordPress needs one |
| `--title` | the domain |
| `--path` | `./<domain>` |
| `--connection` | the default database connection |

The directory must be empty. An installer that writes into somebody's existing
files is one bad argument away from destroying a site.

## What you get that a manual install does not

**A real cron.** WordPress's own scheduler only fires when somebody loads a
page, so on a quiet site scheduled posts and updates simply wait. Servlo
disables the pseudo-cron and installs a one-minute system cron in its place.
See [Cron](/usage/cron).

**A pinned, verified release.** The version and its sha256 are recorded in the
app definition rather than fetched as "latest", so the same command installs the
same thing next month, and a tampered download fails rather than installing.

**A database user scoped to this site.** Not the root account. If this site is
ever compromised, its credentials are worth only its own schema — which is the
main thing standing between one bad plugin and every other site on the server.

**A deploy that will not delete the uploads.** `wp-content/uploads` and
`wp-content/plugins` are excluded by default, because the client installs
plugins and uploads media through wp-admin and the first deploy after that would
otherwise wipe them. See [Deploy](/usage/deploy).

## Bringing an existing WordPress site

Already running somewhere else, with files and a database dump:

```bash
servlo import site /srv/oldblog myblog.example.com --dump ~/oldblog.sql
```

Servlo works out the document root, registers the site, writes the vhost, and
loads the dump into a database of its own. The files are not copied — a
directory you have just uploaded a few gigabytes into is not one to duplicate
for no reason. See [Importing a site](/usage/import).

Then point DNS at this server and run `servlo secure`.

## Verify

```bash
servlo sites          # myblog.example.com, active
servlo status         # nginx, PHP-FPM, mysql
servlo site:doctor    # app-level checks for this site
```

Logs for the site are in the dashboard, or `servlo logs`.

## Next steps

- [Domains](/usage/domains) — aliases, the canonical www form, redirects
- [Backups](/usage/backups) — scheduled, and verified by restoring
- [Deploy](/usage/deploy) — the exclude list matters most for WordPress
- [Database](/usage/database) — `servlo db:import`, `servlo db:shell`
- [Security](/usage/security) — firewall, fail2ban, SSH keys
