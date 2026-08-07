# App Definitions

An **app** is the fourth way to create a site, alongside an existing folder, a clone and a ZIP upload: one click fetches the application, creates its database, writes its config file and sets up its admin account.

Which application, and every detail of how, is YAML in `stores/apps/`. No app's name appears anywhere in servlo's Go, which is what makes a new app a store change rather than a release. A test enforces that: every app the store ships is looked for in every Go file in the engine, and a match fails the build.

## Where they live

```
stores/apps/index.json     what exists, read before fetching any of it
stores/apps/<name>.yaml    one definition per app
```

The definitions are embedded in the binary, so a fresh install has every app it shipped with. A definition published since that build is fetched over HTTP, the same way frameworks and services are.

## A definition

```yaml
name: example                       # lowercase; becomes a filename and a URL segment
label: Example CMS
description: What it is, in a sentence.

framework: example-framework        # the entry in stores/frameworks this maps onto

source:
  version: "7.0.3"                  # pinned, never "latest"
  url: https://downloads.example.org/example-7.0.3.zip
  sha256: 01c5afff…                 # 64 hex characters
  strip_prefix: example             # the wrapper directory inside the archive

secrets:                            # values servlo generates for the template
  - name: salt_auth_key
    length: 64

database:
  required: true
  charset: utf8mb4

config_file:
  path: config.php                  # relative to the site root
  mode: "0600"
  template: |
    <?php
    define( 'DB_NAME', '{{db_name}}' );
    define( 'AUTH_KEY', '{{salt_auth_key}}' );
```

## What is refused, and why

A definition is validated when it is read, not when it is installed. A problem found halfway through an install has already downloaded a release and created a directory on somebody's server.

| Refused | Reason |
|---|---|
| No `version`, or `version: latest` | An installer that fetches whatever is current installs a different thing every week and cannot be verified at all |
| No `sha256`, or one that is not 64 hex characters | An unverified release is a supply-chain hole on a machine serving other people's sites |
| A `url` that is not `https` | The server fetches it; plain HTTP lets anything on the path swap the release, and the checksum only helps if the definition carrying it arrived intact |
| A `config_file.path` that climbs out of the site, is absolute, or carries a backslash | The path comes from a definition, and it decides where servlo writes |
| A `config_file.mode` readable by group or other | It holds the database password, and every site on the machine runs as the same user |
| A secret shorter than 32 or longer than 128 characters | Short enough to guess is worse than absent, because it still looks like a secret to whoever reads the file |
| A template placeholder servlo has no value for | Better than shipping a literal placeholder into production |

## Placeholders

The config template substitutes <code v-pre>{{key}}</code>. Available keys are the database connection, the site's URL, and every secret the definition declared:

<code v-pre>{{db_name}}</code>, <code v-pre>{{db_user}}</code>, <code v-pre>{{db_password}}</code>, <code v-pre>{{db_host}}</code>, <code v-pre>{{site_url}}</code>, plus one per entry in `secrets`.

A value carrying a quote, a backslash or a newline is refused rather than substituted: these land inside quoted strings in a file the application then executes. The alphabet generated secrets are drawn from excludes those characters for the same reason, so a generated value can never be one the renderer would refuse.

## Fetching a release

Nothing is written into the site until the whole download has been read and its checksum has matched, so a release that fails verification leaves the directory as empty as it found it. The download is bounded, and the archive is unpacked by the same extractor an operator's own ZIP upload goes through, with the same refusals: no entry that escapes the directory, no symlinks, no modes carried in from the archive.

`strip_prefix` is checked rather than inferred. Every release archive is wrapped in a directory named for the project, and extracting that verbatim would put the document root one level below where the vhost looks. Naming it means a future release that stops shipping a wrapper fails loudly instead of installing something subtly wrong.

## What an install does, in order

1. **Fetch and verify** the release, and unpack it into the empty site directory.
2. **Create the database** and its user, if the definition asks for one.
3. **Write the config file**, with the connection and the generated secrets substituted in.
4. **Register the site**, generating its vhost and starting its runtime.
5. **Run the setup form** against the now-serving site.

The order is deliberate: each step is undoable only by the step that has not happened yet. Verifying the release before touching the database means a bad checksum leaves no orphaned database behind, and writing the config before registering the site means nothing is served until it is configured.

A database is a *connection*, not an assumption about a local container, so an app installed against an external managed database takes the same path as one using a container on the box.

## The setup step

Every application worth installing already has a setup flow that creates the first account. Reimplementing it means writing that application's user table from Go, which is app-specific and wrong the first time its schema changes, so the definition describes the form instead:

```yaml
setup:
  path: /install.php?step=2          # site-relative, always
  success_contains: "Success"        # what the response must carry
  fields:
    user_name: "{{admin_user}}"
    admin_password: "{{admin_password}}"
    admin_email: "{{admin_email}}"
```

Two refusals matter here.

The path must start with a single `/`. A definition naming a host, including the scheme-relative `//host/path`, would have servlo post an admin password generated seconds earlier to a server of the definition's choosing. Anything that survives the check is appended to the site's own URL, and Go's URL parser keeps the host as the site's whatever the path spells.

`success_contains` is required. Without something to check for, any response at all reads as success, and a setup that silently did not happen leaves an uninstalled application on a live domain for the first passer-by to claim.

A failure reports what the application said, with every value servlo put into the request removed from it first. An application echoing its own form back into an error page is not hypothetical, and that error reaches the panel and the audit log.

## Adding an app

Copy the closest existing definition. Pin the version, and get the checksum from the project's own published one rather than computing it from a single download and trusting it — a checksum you calculated yourself only proves the file did not change between your download and your paste.

Then verify it the only way that counts: install the app on a real server and log into it. An app installer that has never been run is not finished.
