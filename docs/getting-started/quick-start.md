# Quick Start

After `servlo install`, two commands cover every project type:

```bash
# Park a directory, every PHP project inside gets a .test domain automatically
servlo park ~/Servlo

# Confirm DNS, nginx, services, and certs are healthy
servlo status
```
{ .annotate }

1. `servlo park` registers the directory with the watcher service. Every subdirectory that looks like a PHP project gets a `.test` domain, no `/etc/hosts` edits, DNS is handled by dnsmasq running in a Podman container.
2. `servlo status` shows a health summary: DNS, nginx, PHP-FPM containers, services, and cert expiry.

If you only want to register a single project, `cd` into it and run `servlo link` instead of `servlo park`.

---

## Pick your framework

The next steps depend on what you're building. Each walkthrough is end-to-end, from an empty directory to an HTTPS site with a database, dependencies installed, and workers running:

- [**Laravel**](laravel.md): built-in framework, queue + scheduler + Reverb workers
- [**Symfony**](symfony.md): Doctrine migrations, Messenger worker, MySQL or Postgres
- [**WordPress**](wordpress.md): manual `wp-config.php` setup, MySQL
- [**Containers**](containers.md): Node, Python, Go, Ruby, or any runtime via a per-project `Containerfile.servlo`

Already cloning an existing repo with a committed `.servlo.yaml`? `cd` in and run `servlo setup`; it reads the config and runs every install/migrate/build step automatically. See [Project Setup](../features/project-setup.md).

---

## Non-PHP projects

Servlo isn't just for PHP. Drop a `Containerfile.servlo` at the root of any project, run `servlo init`, and Servlo builds the image, runs the container, and reverse-proxies nginx to it with HTTPS, services, and workers exactly like a PHP site.

```dockerfile
# Containerfile.servlo
FROM node:20-alpine
RUN npm install -g nodemon
CMD ["npm", "run", "start:dev"]
```

```bash
cd ~/projects/nestapp
servlo init      # wizard asks for port, containerfile, HTTPS, services
servlo link      # builds image, starts container, wires up nginx
```

See the [Containers walkthrough](containers.md) for Node, Python, Go, and Ruby end-to-end examples, and the Custom Containers reference for every configuration knob.

---

## Add extra services

Servlo ships with **MySQL, PostgreSQL, Redis, Meilisearch, RustFS, and Mailpit** built in. Need anything else? The [**Services walkthrough**](services.md) has copy-paste recipes for the most common ones:

- **MongoDB**: document store with per-site database auto-provisioning
- **phpMyAdmin / pgAdmin / Adminer**: web UIs for MySQL and PostgreSQL
- **Elasticsearch**: full-text search
- **RabbitMQ**: message broker with management UI

Each recipe is a single YAML file plus `servlo service add` and `servlo service start`, that's it.

---

## Web UI

The dashboard is available at **`http://127.0.0.1:7073`** once Servlo is installed. It gives you a visual overview of all your sites, services, and system health.

See [Web UI](../features/web-ui.md) for details.
