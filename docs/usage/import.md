# Importing an existing site

A directory of files and a `.sql` dump become a site here.

```bash
servlo import site /srv/uploads/acme acme.com --dump /srv/uploads/acme.sql
```

That is what somebody actually has when they are moving off shared hosting or off another panel: a tarball they unpacked and a dump they exported. Not a git remote, not a backup in servlo's own format, not an API on the old host. Those cases exist and are handled elsewhere; this is for when all anybody could get out of the old place was files and data.

## What servlo adds

Everything around them. It looks at the files and works out the framework and the document root, registers the site, writes the vhost, and loads the dump into a database of its own with an account scoped to it.

```
Framework      laravel
Document root  public
PHP            8.4
Database       acme
```

Anything it could not work out is said rather than guessed at. A framework it does not recognise means the site has no workers, no deploy script and no health checks until you set one, and it says so.

## The files are not moved

Servlo registers the directory where it already is. A directory somebody has just uploaded a few gigabytes into is not one to copy again for no reason, and a second copy is a second thing to keep in step.

Put them where the site should live before importing.

## The dump

Plain SQL. A compressed dump is refused by name rather than by failing halfway through the load:

```
acme.sql.gz is compressed. Unpack it first: servlo loads plain SQL
```

That is the commonest thing to hand over by mistake, because the filename looks right and the content is not what any client tool will read.

The dump is checked before anything is registered, so an import that cannot work leaves nothing behind to clean up. Once the site exists, a dump that fails to load is reported without unregistering the site: the site is serving, and only the data did not arrive.

It streams from the file into the engine's client, so a dump larger than the droplet's memory imports.

## Afterwards

Point DNS at this server, then `servlo secure acme.com`.

Two things worth checking before you do:

- **The document root.** If servlo reported the site directory itself and the application serves from a subdirectory, set it before DNS points here, or the source is downloadable.
- **The `.env`.** The imported one still holds the old host's database credentials. `servlo env` rewrites the servlo-managed keys; anything else the old host set is yours to review.
