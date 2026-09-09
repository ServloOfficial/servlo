# Directory Layout

```
~/.config/servlo/
└── config.yaml

~/.config/containers/systemd/        # Podman Quadlet units (auto-loaded), 0700
~/.config/systemd/user/
└── servlo-watcher.service

~/.local/share/servlo/
├── bin/                             # fnm, composer, static PHP binaries
├── nginx/
│   ├── nginx.conf
│   ├── conf.d/                      # one .conf per site (auto-generated)
│   ├── custom.d/                    # user overrides, preserved across updates
│   └── logs/
├── certs/
│   ├── ca/
│   └── sites/                       # per-domain .crt + .key
├── data/                            # Podman volume bind-mounts
│   ├── mysql/
│   ├── redis/
│   ├── postgres/
│   ├── meilisearch/
│   └── rustfs/
│   └── servlo.conf
├── vapid-private.key                # Web Push signing key (mode 0600, see features/notifications.md)
├── vapid-public.key                 # Web Push public key, served to browsers
├── push-subscriptions.json          # Browser push subscriptions + per-category prefs (mode 0600)
└── sites.yaml
```

All directories follow the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/). Servlo never writes to system directories except during `servlo install` (DNS setup) which requires `sudo`.
