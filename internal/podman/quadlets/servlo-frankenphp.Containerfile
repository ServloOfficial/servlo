# Servlo FrankenPHP image: the upstream dunglas/frankenphp base plus the same
# runtime PHP extensions the servlo FPM image ships, so a FrankenPHP (Octane) site
# has redis/gd/pdo/intl/... plus the runtime extension set
# tooling. {{.Version}} is the PHP minor (e.g. 8.4); the extension list, extra
# packages, and mkcert CA are injected by the builder.
#
# pcov is intentionally excluded: its coverage runs only via CLI, which execs
# into the shared FPM container, so it stays FPM-only.
FROM docker.io/dunglas/frankenphp:php{{.Version}}-alpine

# User-requested extra Alpine packages (servlo php:pkg) plus any apk build deps a
# custom extension needs to compile. Installed BEFORE the extension loop below so
# a custom extension that builds on FPM can find its deps here too; empty until
# opted in.
{{.CustomPackages}}

# Runtime extensions + Xdebug. install-php-extensions (shipped in the base) builds
# each for the ZTS runtime, pulls its runtime libs, and trims the build toolchain.
# The core set installs in one step; the PECL-built extensions, any user custom
# extension, and xdebug install one-at-a-time and tolerantly, so a single one that
# can't build on this base degrades to "extension missing" instead of bricking the
# whole image (mirroring the FPM build's per-PECL `|| true`). Xdebug loads but
# stays inert until its bind-mounted 99-xdebug.ini arms it, like the FPM image.
RUN install-php-extensions {{.CoreExtensions}} \
    && for ext in {{.OptionalExtensions}}; do \
         install-php-extensions "$ext" \
           || echo "WARN: optional PHP extension $ext unavailable for PHP {{.Version}}, skipping"; \
       done \
    && rm -rf /tmp/* /var/cache/apk/*

# nodejs+npm so the Octane file-watcher (servlo octane:reload on) and JS tooling
# work without an apk add at container start; git + openssh-client so git over
# SSH (private composer packages, source installs run inside this container)
# works without auth failures. All matching the FPM image.
RUN apk add --no-cache nodejs npm git openssh-client && rm -rf /var/cache/apk/*

