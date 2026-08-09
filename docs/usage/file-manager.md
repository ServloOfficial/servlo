# File manager

Every site has a **Files** tab. It browses the site's directory, opens a text file in an editor, uploads, extracts a zip, fixes permissions and deletes. It is scoped to one site's directory and nothing else.

It is also the most dangerous everyday thing in the panel, so the tab carries a warning that is always on screen: this is the live site, every change takes effect immediately, and there is no undo and no backup. That banner is not dismissible on purpose.

---

## What "scoped to one site" means

Every path that arrives in a request is resolved and then checked, and "resolved" is the load-bearing word. A path is refused when it climbs out with `..`, when it is absolute, when cleaning it turns it into an escape, and when a symlink somewhere along it points outside the site. The last one is the case a naive check misses: a link named `uploads` pointing at `/etc` is not spelled `../` anywhere.

A symlink that leaves the site is **listed** but never followed. You will see it in the listing with a "Points outside the site" marker, and everything except deleting the link itself is refused. Hiding it would leave you wondering where a file went; following it would be the escape.

What this is not: a boundary between sites. Every site on a Servlo machine runs as the same Linux user (PRD §6). The file manager confines itself to one site because that is the right shape for the feature, not because the operating system would stop it. Anyone who can reach the file manager for one site is, at the operating-system level, capable of reaching every site; what stops them is Servlo's own path check and the panel's permissions.

## Permissions

The Files tab is site-scoped in `internal/authz`: an Admin reaches every site, a Developer reaches the sites assigned to them. Every write is recorded in the audit log with the file it touched, so `servlo audit` (or the panel's audit view) answers "who changed wp-config.php" and not just "somebody wrote to that site".

## Editing a file

Files up to 2 MB open in the editor. Larger ones are shown truncated and cannot be saved from here, because saving what the browser has would silently discard the rest. Binary files are not opened at all.

Saving is atomic: Servlo writes a temporary file beside the target and renames it, so a failed write leaves the previous contents rather than half the new ones on a site that is serving traffic. A file keeps the mode it already had, except that a secret is pulled back to `0600` whatever it was before.

## Uploading

One file at a time, into the directory you are looking at, up to 128 MB. The filename the browser sends is not trusted: only its base name is used, and a name that still looks like a path is refused. Uploading over an existing file replaces it.

## Extracting an archive

A `.zip` already in the site gets an **Extract** action, which unpacks it into the directory it sits in, keeping its top-level directory. That is deliberately different from adding a site from a ZIP, which strips a single wrapping directory: dropping `acme-plugin.zip` into `wp-content/plugins` should give you `wp-content/plugins/acme-plugin`, not the plugin's contents scattered across the plugins directory.

Every entry is checked before a byte is written: no absolute paths, no `..`, no backslashes, no symlinks, no more than 512 MB expanded and no more than 40,000 entries. Each destination is put through the same path check as everything else, so an archive cannot land through a symlink that leaves the site either. An archive that fails any of these is refused whole, before extraction starts.

Existing files are overwritten, because updating a plugin in place is the case this exists for. Nothing else in the destination is touched.

## Fix permissions

The temptation here is `chmod -R 755`, and that is exactly the wrong tool: it makes every `.env` world-readable on the way past and strips the execute bit off `artisan` and everything in `vendor/bin`. So Servlo applies four named rules, shows you the plan before it does anything, and leaves alone anything no rule claims.

| Rule | Mode | Why |
|---|---|---|
| Directories | `0755` | nginx and PHP-FPM have to traverse the tree to serve anything under it |
| Files | `0644` | readable by the server, writable only by the account that owns the site |
| Executable files | `0755` | a file that is already executable stays executable, so `artisan` and `vendor/bin` keep working |
| Secrets | `0600` | `.env`, `.env.*`, `.htpasswd`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `id_rsa` and friends |

Not touched: symlinks (chmod follows them, and following one is the only way this pass could leave the site), sockets, fifos, device nodes, and everything under `.git`, which keeps its own modes and whose hooks stop running if you flatten them to `0644`.

The plan names, for each rule, how many paths it claims, how many are actually wrong today, and a few examples. A site larger than 200,000 paths is walked as far as that and says so, rather than holding the request open.

## Deleting

Deleting asks you to type the name back. It removes from a live site with no undo and no backup, so a modal you can dismiss with the same reflex that opened it would not be friction. Deleting a directory removes everything inside it.

Deleting a symlink removes the link, not what it points at.

The site root itself cannot be deleted from here. Removing a site is a different operation with its own confirmation.

## What is not here

No rename, no new folder, no move or copy, no download, and no per-file mode editor. These were left out of the first version deliberately; the path-safety work they would all share is done, so they are small additions when there is a reason for them.
