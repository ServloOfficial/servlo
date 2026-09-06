# Nginx Overrides

Servlo generates one nginx config per site under `~/.local/share/servlo/nginx/conf.d/{domain}.conf`. These files are fully managed: `servlo link`, `servlo secure`, `servlo site rebuild`, and every `servlo install` (including the one that runs at the end of `servlo update`) regenerate them from the built-in templates. Any edits made directly to those files are overwritten.

To add per-site directives that survive every regeneration, drop a snippet in `~/.local/share/servlo/nginx/custom.d/`, or edit it from the web UI. Because editing nginx is something you reach for rarely, it is no longer a tab: open the site detail page and click the sliders button at the end of the address bar to open the editor in a large modal. The editor is the same surface as the **Env** tab: changes go through a confirmation modal with an optional "back up the current file first" checkbox, and if a backup exists a **Restore** button opens a diff modal before rolling the override back. Backups are written to a sibling `custom.d.bkp/` directory (the live `custom.d/` is auto-included by every site vhost via a `{domain}.conf*` glob, so backups have to live outside of it to avoid being loaded as duplicate directives) and the most recent one is consumed when you restore. Every save runs `nginx -t` inside the servlo-nginx container before the new bytes are committed to the live config: if validation fails, the file is rolled back to its previous contents (or removed if there was no previous file), the staged backup is dropped, and nginx's diagnostic is shown in the save modal so you can fix the line and retry without leaving the live nginx config broken.

An override is for what *one site* needs. When every site of a framework needs the same directives, as Magento does for `/setup`, `/static`, and `/media`, that belongs in the framework definition's `nginx.snippet` instead, which is spliced in near the top of the server block. See [framework definitions](framework-definitions.md).

## Site settings, before you reach for an override

Most of what a site needs from nginx is a field on its **Settings** tab, and a field is safer than a snippet: servlo knows what the value has to be valid for, and writes every directive the setting needs rather than the one you happened to think of.

| Field | What it writes |
|---|---|
| Max upload size | nginx `client_max_body_size`, and PHP's `upload_max_filesize` and `post_max_size` |
| Max execution time | nginx `fastcgi_read_timeout` and `fastcgi_send_timeout`, and PHP's `max_execution_time` |
| Cache static assets | an `expires` and `Cache-Control` block for CSS, JS, images and fonts |
| Response headers | an `add_header ... always` per header, on every response including error pages |

The upload and execution fields are the PHP ones described under [per-site PHP settings](php.md); they appear here too because half of each lands in nginx.

Static-asset caching never matches `.php` or `.html`. A PHP file cached for a month is a site that cannot be deployed, and an HTML page is usually the one file that has to be able to change now.

The cache block repeats the site's response headers and its HSTS inside itself, which looks redundant and is not: nginx's `add_header` does not merge downward, so a location that sets one discards every header inherited from the server block. Without the repetition, turning caching on would silently drop a site's security headers for exactly the files a browser fetches the most.

`Strict-Transport-Security` is refused as a response header. Servlo writes it from the site's TLS state, `add_header` appends rather than replaces, and a browser is entitled to honour the shorter `max-age` of the two, so the form could quietly weaken it.

The raw editor sits underneath the fields on the same tab, for everything they do not cover.

## Generated vhosts are validated too

The override above is not the only file that gets `nginx -t` before it counts. Every generated vhost servlo writes goes through the same commit: the previous contents are kept as a timestamped backup in `~/.local/share/servlo/nginx/conf.d.bkp/`, the new file is written, and nginx is asked whether it will load. If nginx objects to *this* file, the previous contents go back and the diagnostic comes back with the error. nginx loads its whole configuration or none of it, so one refused file is every site on the machine down, not just the one whose file it is.

Backups live in `conf.d.bkp/` rather than beside the live file because nginx includes `conf.d/*.conf`, and a backup kept there would load as a second server block for the same domain.

Two things deliberately do not roll back. A failure naming some other file is somebody else's broken config, and reverting this site would lose a good change without fixing anything. And a container that cannot be asked at all is the ordinary state during install and the first link, where refusing to write would mean a site never gets the vhost nginx needs in order to start.

A regeneration whose bytes match what is already on disk does nothing: no write, no backup, no validation. Every link, install and quadlet rewrite regenerates every vhost, so without that the backup directory would fill with copies of one file.

Restoring a generated vhost puts a backup back through the same validation and reloads. It is for getting a site serving again now, not for holding config against servlo: the next regeneration overwrites it. For changes meant to last, use the override above, or the site's Settings tab.

## From the CLI

The same override is reachable without the web UI, which is handy for scripting. `servlo nginx show [site]` prints the current override (`--path` prints just the file path), `servlo nginx edit [site]` opens it in `$EDITOR` and then validates with `nginx -t` and reloads on save, and `servlo nginx reset [site]` deletes it and falls back to the bundled defaults. Both surfaces go through one shared edit service, so validation, backups, and reload behave identically whichever one you use.

## How it works

Every generated site vhost ends with:

```nginx
include /etc/nginx/custom.d/{your-domain}.conf*;
```

The trailing `*` makes the include a glob, so nginx treats a missing override file as empty (no 500). The directory is bind-mounted read-only into the `servlo-nginx` container and is never touched by servlo after creation.

## Request timeouts

By default nginx waits 60 seconds for a request to complete before returning `504 Gateway Timeout`. Apps with deliberately long-running requests (heavy reports, slow third-party calls, hardware that answers on its own schedule) need that raised, and this does not need a `custom.d` snippet.

Set `request_timeout` in seconds and servlo writes it straight into the generated vhost. Globally, in `~/.config/servlo/config.yaml`:

```yaml
nginx:
  request_timeout: 300
```

or per project in `.servlo.yaml`, which overrides the global value and travels with the repo:

```yaml
request_timeout: 300
```

servlo renders this into `fastcgi_read_timeout` and `fastcgi_send_timeout` for PHP-FPM sites, and into `proxy_read_timeout` and `proxy_send_timeout` for proxy, FrankenPHP, and custom-container sites, so the same setting works whatever runtime the site uses. Run `servlo link` (or `servlo install`) afterwards to regenerate the vhost and reload nginx.

A long nginx timeout only helps if PHP is allowed to run that long too. For requests that are CPU-bound rather than waiting on I/O, also raise `max_execution_time` in the per-version php.ini via `servlo php:ini <version>`.

## Example: raise the upload limit for one site

Create `~/.local/share/servlo/nginx/custom.d/bigapp.example.com.conf`:

```nginx
client_max_body_size 200m;
```

Then reload nginx so the include picks it up. The `custom.d` directory is read by the nginx container, so restart that container (`servlo restart` only restarts a site's PHP-FPM container and will not pick up a `custom.d` change):

```sh
systemctl --user restart servlo-nginx
```

That's it. The snippet is merged into the generated server block for `bigapp.example.com` and nothing servlo does afterwards (including a version upgrade) will touch it.

## Scope

Lines you put in `custom.d/{domain}.conf` land inside the site's `server { ... }` block, so you can use anything nginx allows at server level: `client_max_body_size`, `add_header`, extra `location` blocks, `proxy_pass` overrides, `rewrite`, and so on.

Two things there cannot be redeclared, because the generated vhost already carries them and nginx rejects the repeat rather than overriding it. Setting `root` fails with `"root" directive is duplicate`; servlo derives the docroot from the linked site, so change it with `servlo link` rather than from a snippet. Repeating a `location` servlo emits (`/`, `~ \.php$`, `~ /\.ht`) fails with `duplicate location`; add a differently scoped location such as `/media` instead of restating one of those. Both failures surface in the editor before anything is committed, so the site keeps serving on its previous config.

If you need directives at `http {}` level (gzip, proxy buffers, a global `client_max_body_size`, a new `map`), edit the **global override** from the web UI: open **System → Nginx** and pick the **Config** tab. It edits `~/.local/share/servlo/nginx/http.d/zz-servlo-user.conf`, which the generated `nginx.conf` includes last inside its `http {}` block, after servlo's own settings. The editor is the same surface as the per-site one: Save runs `nginx -t` before the new bytes are committed, there is an optional backup-first checkbox with a **Restore** button, and **Reset** drops the file back to empty. The `http.d` directory is bind-mounted read-only into the `servlo-nginx` container and servlo never writes into it itself.

nginx does not let a later `http {}` directive win over an earlier one: a second `client_max_body_size` in the same block is a hard `directive is duplicate` error, not an override. So when your override declares a directive servlo also ships at `http {}` level (`client_max_body_size`, `sendfile`, `keepalive_timeout`, the `server_names_hash_*` pair), servlo comments its own default out of the generated `nginx.conf` and leaves the field to you. Reset the override and the default comes back. This happens while the file is saved, so if you edit `zz-servlo-user.conf` by hand instead, run `servlo start` afterwards to regenerate `nginx.conf` before restarting the container.

`log_format` and `access_log` are the exception, because nginx allows either to appear more than once in the same block. Declaring your own adds to servlo's rather than colliding with it, so both are left in place: your log keeps its format and servlo keeps the `servlo_access` feed that per-site request timing reads. Retiring servlo's `log_format` would leave its `access_log` naming a format nginx no longer knows, which fails the config check and takes every site down.

Note that servlo's shipped `client_max_body_size` is `0`, meaning unlimited, so setting a value of your own can only make uploads stricter. If large uploads are failing without an override in place, the limit is PHP's, not nginx's: PHP defaults to a 2M `upload_max_filesize` and an 8M `post_max_size`. Raise both for every site and every PHP version with `servlo php:ini shared` (or the **shared** scope of the php.ini editor under System → PHP), where the two keys are already waiting as commented lines. Uncomment them, save, and FPM restarts with the new limits.

Prefer keeping it on disk? Drop a snippet into `~/.local/share/servlo/nginx/conf.d/` with a filename that starts with an underscore (e.g. `_myorg.conf`). Files in `conf.d/` that servlo does not know about are left alone during regeneration.

## Customising the catch-all (`_default.conf`)

The catch-all vhost servlo ships for domains that reach this server without being linked to a site lives at `~/.local/share/servlo/nginx/conf.d/_default.conf`. Editing it directly is supported: servlo stamps a hash sidecar (`_default.conf.servlo-managed-hash`) when it first writes the file, then compares your on-disk content to that hash on every subsequent `servlo start`. If the hashes match, servlo keeps the file in sync with template changes; if they differ, your edit is preserved and the next start logs that it skipped the rewrite. Delete the conf (or the sidecar) to restore servlo's default. A common reason to edit it is swapping `ssl_reject_handshake on;` for `ssl_reject_handshake off;` on a staging machine where you want unlinked HTTPS hostnames to receive a 444 close rather than a TLS alert.

## Forwarded headers and tunneling

The generated vhosts already set the `X-Forwarded-*` family for you, so an upstream proxy or load balancer works out of the box:

| Forwarded source | Where it comes from |
| --- | --- |
| `HTTP_HOST`, `SERVER_NAME`, `HTTP_X_FORWARDED_HOST` | `$http_x_forwarded_host`, falling back to `$host` |
| `HTTP_X_FORWARDED_PROTO` | `$http_x_forwarded_proto`, falling back to `$scheme` |
| `HTTP_X_FORWARDED_PORT` | `$server_port` |
| `HTTP_X_REAL_IP`, `HTTP_X_FORWARDED_FOR` | `$remote_addr` |

The fallbacks are declared once in `conf.d/_forwarded.conf` (generated by servlo at install time) via two `map` blocks that produce `$real_forwarded_host` and `$real_forwarded_proto`. Direct browser requests without `X-Forwarded-*` headers keep seeing the real host and scheme; tunneled requests see the public hostname the tunnel received. PHP apps that call `url()` or read `$_SERVER['HTTP_HOST']` get correct absolute URLs in both paths without any app-side changes.
