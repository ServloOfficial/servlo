# Production mode

Production mode is one flag several other decisions read: whether PHP shows errors to visitors, whether OPcache trusts its cache without re-checking the filesystem, and how a worker restarts when it dies.

They belong together because they are the same question asked several ways. Setting them individually is how a machine ends up half-production, showing stack traces to the world while caching hard enough that nobody notices the fix.

```bash
servlo production          # show the current state
servlo production on       # asks for confirmation; --yes skips it
servlo production off --force
```

The dashboard header carries a badge for the current state, so it is visible from every page rather than only from a settings tab.

## Why turning it off is the harder direction

Turning production mode **on** is confirmed, because it changes what visitors see. Turning it **off** needs `--force`, because doing it on a live machine starts showing PHP stack traces to the internet, and that is not something to do by autocompleting a command.

`--force` is a flag rather than a prompt on purpose: a prompt can be answered by muscle memory, or by a script piping `y`.

## What it changes

| | Production off | Production on |
|---|---|---|
| `display_errors` | On | Off |
| `display_startup_errors` | On | Off |
| `expose_php` | On | Off |
| `opcache.enable` | 1 | 1 |
| `opcache.validate_timestamps` | 1 | 0 |
| Worker restart policy | as declared | `always` |

The PHP settings are written to a drop-in named `90-production.ini`, which sorts ahead of the shared and per-site files so a site can still override any of them. Changing the mode writes the drop-in immediately, rather than at the next restart, so what is on disk always matches the flag; applying it to running containers is `servlo restart`.

`opcache.validate_timestamps=0` is the setting that surprises people. With it off, PHP never re-reads a changed file, which is most of the speed. It also means a deploy that does not reload PHP-FPM serves the old code indefinitely. Servlo's deploy reloads for you.

## Service hardening

Databases, caches, search engines and admin UIs bind to **loopback only**, always. This is a design law rather than a setting: a database reachable from off the machine is a database anyone who finds the port can attack, and on a box hosting other people's sites there is no version of that worth the convenience.

Only nginx ever binds beyond loopback, because only nginx has a reason to: it serves the sites. That is not a setting either, in either direction. A quadlet that arrives already bound to every interface, from an edit by hand or an older install, is pulled back to loopback rather than left as it is, and nginx is pushed out to every interface rather than left on loopback where it would answer nobody.

Containers reach each other over the `servlo` Podman network by name (`servlo-mysql`, `servlo-redis`), which does not involve a published host port at all. The published loopback port exists for host tools: a GUI client, a `psql` on the droplet, an SSH tunnel from your laptop.

```bash
ssh -L 3306:127.0.0.1:3306 you@your-droplet
```

That is the supported way to reach a database from elsewhere, and it authenticates before anything touches the database.

## Service passwords

Every service credential is generated once per install, stored `0600` at `~/.config/servlo/service-password`, and substituted into the service definition wherever it declares `{{password}}`.

Presets ship the placeholder, never a value:

```yaml
environment:
  MYSQL_ROOT_PASSWORD: "{{password}}"
env_vars:
  - DB_PASSWORD={{password}}
connection_url: mysql://root:{{password}}@127.0.0.1:{{host_port}}/servlo
```

Quote it when it is the whole value of a YAML field. Bare braces are a YAML flow mapping rather than a string, so `MYSQL_ROOT_PASSWORD: {{password}}` does not parse.

The substitution happens where the definition is read, before it is parsed, so the password reaches every part of it: container environment, the variables written into a site's `.env`, connection URLs, the command a service is launched with, and mounted config files. A definition fetched from the store goes through the same path as a built-in one.

One password per install rather than one per service, because the definitions cross-reference each other: phpMyAdmin has to log into MySQL, RedisInsight into Redis, and a per-service secret would need a resolution pass those definitions have no way to express.

To read it, ask for the service's environment:

```bash
servlo service start mysql   # prints the .env lines, password included
cat ~/.config/servlo/service-password
```

::: warning A database keeps the password it was initialised with
MySQL and PostgreSQL apply their root password once, when the data directory is first created. Changing the file afterwards does not change the password of a database that already exists; it would leave the container on the old credential while every `.env` was rewritten with the new one. Rotating an existing database's password is a database operation, not a file edit.
:::
