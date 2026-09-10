# Logs

Every container and unit servlo runs writes somewhere it can be read back, so debugging a broken site doesn't mean hunting for files by hand. Two surfaces read them: the CLI and the dashboard.

## From the CLI

```sh
servlo logs              # the current project's PHP-FPM container
servlo logs nginx        # the nginx container
servlo logs mysql        # any installed service
servlo logs 8.4          # a specific PHP-FPM container, for site-less sessions
```

`--follow` / `-f` streams new output as it arrives, and `--lines` / `-n` sets how many lines of history to show first (default 100, `0` for all). Both map onto `podman logs`, so what you see is exactly what the container wrote.

## From the dashboard

The Logs view streams the same output over a WebSocket, one pane per unit, with the container and worker units listed side by side. Framework workers (queue, schedule, horizon, reverb) are systemd user units rather than containers, so their output comes from the user journal; everything else comes from the container.

A pane on a service closes when that service's container goes, which is what a remove, a restart, a reinstall and a version migration all do. The pane reconnects on its own, so a restart looks like a brief gap and then the new container's output. Leaving it attached to a container being torn down is what jams podman.

Application logs your framework writes, such as Laravel's `storage/logs/laravel.log`, are files inside the project and are read directly from disk rather than through either of the above.

## Notes

- A container or unit that isn't running returns whatever output already exists rather than an error.
- Large files are read from a tail rather than in full, so a log that has grown to hundreds of megabytes still opens instantly.
