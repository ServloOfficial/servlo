// Demo runtime stubs — make the real servlo UI run with no backend.
// Imported FIRST (before App) so window.fetch / WebSocket / open are patched
// before any store ever calls them. Everything below is fixtures + a tiny
// in-memory mock backend so clicking around the demo behaves like the app.
import version from './fixtures/version.json';
import sitesFixture from './fixtures/sites.json';
import servicesFixture from './fixtures/services.json';
import presetsFixture from './fixtures/presets.json';
import statusFixture from './fixtures/status.json';
import accessMode from './fixtures/access-mode.json';
import settings from './fixtures/settings.json';
import phpVersions from './fixtures/php-versions.json';
import nodeVersions from './fixtures/node-versions.json';
import phpInstallable from './fixtures/php-installable.json';
import lanStatus from './fixtures/lan_status.json';
import stats from './fixtures/stats.json';
import workersHealth from './fixtures/workers_health.json';
import databasesFixture from './fixtures/databases.json';
import dbConnections from './fixtures/db-connections.json';
import filesFixture from './fixtures/files.json';
import sftpFixture from './fixtures/sftp.json';
import cronFixture from './fixtures/cron.json';
import backupsFixture from './fixtures/backups.json';
import dbUsers from './fixtures/db-user.json';
import smtpFixture from './fixtures/smtp.json';
import alertsFixture from './fixtures/alerts.json';
import appsFixture from './fixtures/apps.json';
import serverStateFixture from './fixtures/server-state.json';
import securityFixture from './fixtures/security.json';

// Demo follows the system theme (auto). Reset any stale value a previous demo
// session may have pinned, so it isn't stuck on a forced light/dark.
try {
  localStorage.setItem('servlo-theme', 'auto');
} catch {
  /* private mode */
}

// Mutable state so mock mutations persist across reloads of the list.
const sites = structuredClone(sitesFixture) as Array<Record<string, unknown>>;
const services = structuredClone(servicesFixture) as Array<Record<string, unknown>>;
const presets = structuredClone(presetsFixture) as Array<Record<string, unknown>>;
// Status is mutable too, so applying a tool update lands on the card that asked.
const status = structuredClone(statusFixture) as Record<string, unknown>;
// Mail settings are mutable too, so a save in the demo lands on the card that
// made it rather than snapping back to the fixture on the next load.
interface DemoSMTP {
  configured: boolean;
  settings: Record<string, unknown>;
  env_file?: string;
  env_keys?: string[];
  note?: string;
}
const smtp = structuredClone(smtpFixture) as { panel: DemoSMTP; sites: Record<string, DemoSMTP> };

function smtpFor(domain: string): DemoSMTP {
  if (!smtp.sites[domain]) smtp.sites[domain] = structuredClone(smtp.sites['default']);
  return smtp.sites[domain];
}

// The password goes in and never comes back, exactly as the real API behaves.
function applySMTP(target: DemoSMTP, raw: string): DemoSMTP {
  const body = JSON.parse(raw || '{}') as Record<string, unknown>;
  const password = String(body.password ?? '');
  target.settings = {
    host: body.host ?? '',
    port: body.port ?? 587,
    username: body.username ?? '',
    has_password: password !== '' || Boolean(target.settings.has_password),
    encryption: body.encryption ?? 'starttls',
    from_address: body.from_address ?? '',
    from_name: body.from_name ?? '',
  };
  target.configured = Boolean(body.host) && Boolean(body.port);
  return target;
}

// Static GET fixtures keyed by exact path.
const ROUTES: Record<string, unknown> = {
  // The panel asks who you are before it renders anything. Without this the
  // demo is a login form, which is what the docs landing page showed from the
  // moment session auth landed until somebody looked.
  '/api/auth/session': {
    authenticated: true,
    user: 'demo',
    role: 'admin',
    csrf: 'demo-csrf-token',
    setup_needed: false,
    totp_enabled: false,
  },
  '/api/version': version,
  '/api/status': status,
  '/api/access-mode': accessMode,
  '/api/settings': settings,
  '/api/php-versions': phpVersions,
  '/api/node-versions': nodeVersions,
  '/api/php-installable': phpInstallable,
  '/api/lan/status': lanStatus,
  '/api/stats': stats,
  '/api/workers/health': workersHealth,
  '/api/db-connections': dbConnections,
};

// ---- Example payloads for the editor / REPL tabs ----
// Hosts are the rootless Podman container names on servlo's shared network, not
// 127.0.0.1 — this mirrors the env servlo actually injects (see services env_vars).
const ENV_TEXT = `APP_NAME="Acme"
APP_ENV=local
APP_KEY=base64:0aF3l9Qx7sample0key0not0real0value0here=
APP_DEBUG=true
APP_URL=https://acme-supply.com

LOG_CHANNEL=stack
LOG_LEVEL=debug

DB_CONNECTION=mysql
DB_HOST=servlo-mysql
DB_PORT=3306
DB_DATABASE=acme
DB_USERNAME=root
DB_PASSWORD=servlo

REDIS_HOST=servlo-redis
REDIS_PORT=6379
REDIS_PASSWORD=null

CACHE_STORE=redis
QUEUE_CONNECTION=redis
SESSION_DRIVER=redis

MAIL_MAILER=smtp
MAIL_HOST=smtp.postmarkapp.com
MAIL_PORT=1025
MAIL_FROM_ADDRESS="hello@acme-supply.com"

SCOUT_DRIVER=meilisearch
MEILISEARCH_HOST=http://servlo-meilisearch:7700

FILESYSTEM_DISK=s3
AWS_ENDPOINT=http://servlo-rustfs:9000
`;

const NGINX_TEXT = `server {
    listen 443 ssl;
    http2 on;
    server_name acme-supply.com;
    root "/home/dev/code/acme/public";

    ssl_certificate     "/home/dev/.config/servlo/certs/acme-supply.com.crt";
    ssl_certificate_key "/home/dev/.config/servlo/certs/acme-supply.com.key";

    index index.php;
    charset utf-8;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \\.php$ {
        fastcgi_pass unix:/home/dev/.config/servlo/run/php8.4-fpm.sock;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
    }

    location ~ /\\.(?!well-known).* {
        deny all;
    }
}
`;

const PHP_INI_TEXT = `; Servlo-managed php.ini overrides — PHP 8.4
memory_limit = 512M
max_execution_time = 120
upload_max_filesize = 64M
post_max_size = 64M
display_errors = On
error_reporting = E_ALL

[opcache]
opcache.enable = 1
opcache.jit = tracing
opcache.jit_buffer_size = 64M

`;

// Per-site request-timing analytics (the site Overview's Request timing view,
// served at /api/sites/<domain>/analytics). The real view reads a durable SQLite
// store; each profile below is expanded into the same shape, with the throughput
// series and recent-request timestamps placed relative to now so the chart and
// list read as live. acme and shopfront run busy with flagged slow routes and a
// cold start; every other site falls back to DEFAULT_ANALYTICS so the panel is
// never empty. Route p95s and the cold flag exercise the severity colours, the
// "cold excluded" note, and the greyed cold row.
const LATENCY_EDGES = [25, 50, 100, 250, 500, 1000];

interface DemoRouteStat {
  route: string;
  method: string;
  example: string;
  p50_millis: number;
  p95_millis: number;
  recent_p95_millis: number;
  multiplier: number;
  samples: number;
}

interface DemoRecent {
  agoSec: number; // seconds before now, so the list reads as live
  method: string;
  route: string;
  uri: string;
  status: number;
  millis: number;
  cold?: boolean;
}

interface AnalyticsProfile {
  samples: number;
  cold_starts: number;
  median_millis: number;
  p95_millis: number;
  status: { c2xx: number; c3xx: number; c4xx: number; c5xx: number };
  distribution: number[]; // one count per LATENCY_EDGES bucket, last is the open >1s bucket
  throughput: number[]; // per-minute counts, oldest first, ending at the current minute
  routes: DemoRouteStat[];
  recent: DemoRecent[]; // newest first
}

// wave builds a smooth per-minute throughput series of length n around avg, so
// each profile gets a realistic curve without a hand-written array.
function wave(n: number, avg: number): number[] {
  return Array.from({ length: n }, (_, i) => Math.max(1, Math.round(avg + avg * 0.5 * Math.sin(i / 2))));
}

const ANALYTICS_PROFILES: Record<string, AnalyticsProfile> = {
  'acme-supply.com': {
    samples: 1846,
    cold_starts: 3,
    median_millis: 72,
    p95_millis: 240,
    status: { c2xx: 1720, c3xx: 88, c4xx: 34, c5xx: 4 },
    distribution: [90, 360, 720, 430, 130, 40, 6],
    throughput: wave(24, 15),
    routes: [
      { route: 'GET /', method: 'GET', example: '/', p50_millis: 78, p95_millis: 150, recent_p95_millis: 138, multiplier: 3.5, samples: 1846 },
      { route: 'POST /checkout', method: 'POST', example: '', p50_millis: 190, p95_millis: 512, recent_p95_millis: 512, multiplier: 12.5, samples: 63 },
      { route: 'GET /orders/:id', method: 'GET', example: '/orders/42', p50_millis: 96, p95_millis: 233, recent_p95_millis: 233, multiplier: 5.7, samples: 214 },
      { route: 'GET /dashboard', method: 'GET', example: '/dashboard', p50_millis: 120, p95_millis: 268, recent_p95_millis: 260, multiplier: 6.4, samples: 96 },
      { route: 'GET /cart', method: 'GET', example: '/cart', p50_millis: 60, p95_millis: 176, recent_p95_millis: 176, multiplier: 4.1, samples: 148 },
      { route: 'GET /products', method: 'GET', example: '/products', p50_millis: 44, p95_millis: 92, recent_p95_millis: 88, multiplier: 1.8, samples: 402 },
    ],
    recent: [
      { agoSec: 4, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 147 },
      { agoSec: 18, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 130 },
      { agoSec: 46, method: 'GET', route: 'GET /products', uri: '/products', status: 200, millis: 70 },
      { agoSec: 62, method: 'POST', route: 'POST /checkout', uri: '/checkout', status: 302, millis: 199 },
      { agoSec: 75, method: 'GET', route: 'GET /cart', uri: '/cart', status: 200, millis: 88 },
      { agoSec: 121, method: 'GET', route: 'GET /orders/:id', uri: '/orders/42', status: 200, millis: 233 },
      { agoSec: 140, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 138 },
      { agoSec: 168, method: 'GET', route: 'GET /dashboard', uri: '/dashboard', status: 200, millis: 268 },
      { agoSec: 205, method: 'GET', route: 'GET /products', uri: '/products', status: 200, millis: 66 },
      { agoSec: 232, method: 'GET', route: 'GET /cart', uri: '/cart', status: 404, millis: 41 },
      { agoSec: 300, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 106 },
      { agoSec: 360, method: 'GET', route: 'GET /orders/:id', uri: '/orders/99', status: 200, millis: 210 },
      { agoSec: 900, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 613, cold: true },
    ],
  },
  'shopfront.io': {
    samples: 921,
    cold_starts: 1,
    median_millis: 44,
    p95_millis: 150,
    status: { c2xx: 900, c3xx: 12, c4xx: 9, c5xx: 0 },
    distribution: [140, 420, 300, 60, 8, 2, 0],
    throughput: wave(24, 9),
    routes: [
      { route: 'GET /', method: 'GET', example: '/', p50_millis: 40, p95_millis: 96, recent_p95_millis: 92, multiplier: 2.4, samples: 921 },
      { route: 'GET /cart', method: 'GET', example: '/cart', p50_millis: 58, p95_millis: 176, recent_p95_millis: 176, multiplier: 3.7, samples: 148 },
      { route: 'GET /catalog', method: 'GET', example: '/catalog', p50_millis: 52, p95_millis: 120, recent_p95_millis: 118, multiplier: 2.9, samples: 260 },
      { route: 'POST /cart/add', method: 'POST', example: '', p50_millis: 70, p95_millis: 150, recent_p95_millis: 150, multiplier: 3.8, samples: 88 },
    ],
    recent: [
      { agoSec: 9, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 62 },
      { agoSec: 33, method: 'GET', route: 'GET /catalog', uri: '/catalog', status: 200, millis: 118 },
      { agoSec: 51, method: 'POST', route: 'POST /cart/add', uri: '/cart/add', status: 200, millis: 150 },
      { agoSec: 88, method: 'GET', route: 'GET /cart', uri: '/cart', status: 200, millis: 176 },
      { agoSec: 140, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 48 },
      { agoSec: 210, method: 'GET', route: 'GET /catalog', uri: '/catalog', status: 200, millis: 96 },
      { agoSec: 720, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 388, cold: true },
    ],
  },
};

// DEFAULT_ANALYTICS is a healthy, populated profile for every other demo site, so
// the panel shows real numbers rather than the empty "watching for requests" card.
const DEFAULT_ANALYTICS: AnalyticsProfile = {
  samples: 816,
  cold_starts: 0,
  median_millis: 30,
  p95_millis: 70,
  status: { c2xx: 804, c3xx: 6, c4xx: 6, c5xx: 0 },
  distribution: [260, 180, 60, 10, 0, 0, 0],
  throughput: wave(24, 6),
  routes: [
    { route: 'GET /', method: 'GET', example: '/', p50_millis: 34, p95_millis: 78, recent_p95_millis: 72, multiplier: 1.9, samples: 420 },
    { route: 'GET /login', method: 'GET', example: '/login', p50_millis: 28, p95_millis: 62, recent_p95_millis: 60, multiplier: 1.6, samples: 96 },
    { route: 'GET /api/health', method: 'GET', example: '/api/health', p50_millis: 8, p95_millis: 18, recent_p95_millis: 16, multiplier: 1.1, samples: 300 },
  ],
  recent: [
    { agoSec: 6, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 34 },
    { agoSec: 24, method: 'GET', route: 'GET /api/health', uri: '/api/health', status: 200, millis: 12 },
    { agoSec: 58, method: 'GET', route: 'GET /login', uri: '/login', status: 200, millis: 60 },
    { agoSec: 132, method: 'GET', route: 'GET /', uri: '/', status: 200, millis: 44 },
    { agoSec: 240, method: 'GET', route: 'GET /api/health', uri: '/api/health', status: 200, millis: 10 },
  ],
};

// Per-site application logs (Logs tab → App logs, the default sub-tab), served
// over REST at /api/app-logs/<domain>[/<file>]. A realistic Laravel run: mostly
// INFO with a WARNING and one ERROR carrying a stack trace, so the expandable
// detail and the level colours both have something to show.
const APP_LOG_FILES = [
  { name: 'laravel.log', size: 48213 },
  { name: 'laravel-2026-07-07.log', size: 15922 },
];

function appLogEntries(): Array<Record<string, unknown>> {
  const now = Date.now();
  const at = (secAgo: number) => new Date(now - secAgo * 1000).toISOString().replace('T', ' ').slice(0, 19);
  return [
    { level: 'INFO', date: at(640), message: 'User authenticated', detail: 'local.INFO: User authenticated {"user_id":42,"guard":"web"}' },
    { level: 'INFO', date: at(600), message: 'Order placed', detail: 'local.INFO: Order placed {"order_id":900,"total":"249.00"}' },
    { level: 'WARNING', date: at(320), message: 'Coupon code not found, ignoring', detail: 'local.WARNING: Coupon code not found, ignoring {"code":"SUMMER"}' },
    { level: 'INFO', date: at(180), message: 'Shipment notification queued', detail: 'local.INFO: Shipment notification queued {"job":"App\\\\Jobs\\\\SendShipmentNotification"}' },
    { level: 'ERROR', date: at(70), message: 'Stripe charge failed: card_declined', detail: 'local.ERROR: Stripe charge failed: card_declined {"exception":"[object] (Stripe\\\\Exception\\\\CardException(code: 402): Your card was declined.)"}\n#0 /app/Services/Billing.php(67): Stripe\\Charge::create()\n#1 /app/Http/Controllers/CheckoutController.php(63): App\\Services\\Billing->charge()\n#2 {main}' },
    { level: 'INFO', date: at(20), message: 'Cache warmed', detail: 'local.INFO: Cache warmed {"keys":128}' },
  ];
}

// analyticsFor expands a profile into the analytics response the view expects,
// stamping the throughput points and recent list with times relative to now.
function analyticsFor(domain: string, range: string): unknown {
  const p = ANALYTICS_PROFILES[domain] ?? DEFAULT_ANALYTICS;
  const now = Date.now();
  const minute = 60_000;
  const nowMin = Math.floor(now / minute) * minute;
  return {
    site: domain,
    range,
    samples: p.samples,
    cold_starts: p.cold_starts,
    median_millis: p.median_millis,
    p95_millis: p.p95_millis,
    status: p.status,
    distribution: p.distribution.map((count, i) => ({ upper_millis: LATENCY_EDGES[i] ?? 0, count })),
    throughput: p.throughput.map((count, i) => ({
      at_millis: nowMin - (p.throughput.length - 1 - i) * minute,
      count,
    })),
    routes: p.routes,
    recent: p.recent.map((r) => ({
      at_millis: now - r.agoSec * 1000,
      method: r.method,
      route: r.route,
      uri: r.uri,
      status: r.status,
      millis: r.millis,
      cold: !!r.cold,
    })),
  };
}

// ---- Overview "Actions" section: per-framework command sets + doctor ----
// The command cards and the doctor card both call per-site endpoints; give them
// framework-appropriate fixtures so the section looks like a real project.
const LARAVEL_COMMANDS = [
  { name: 'migrate', label: 'Migrate', command: 'php artisan migrate', icon: 'database', description: 'Run pending database migrations', confirm: true },
  { name: 'migrate-fresh', label: 'Fresh + seed', command: 'php artisan migrate:fresh --seed', icon: 'refresh', description: 'Drop all tables, re-migrate and seed', confirm: true },
  { name: 'optimize-clear', label: 'Clear caches', command: 'php artisan optimize:clear', icon: 'broom', description: 'Flush config, route, view and event caches' },
  { name: 'key-generate', label: 'App key', command: 'php artisan key:generate', icon: 'key', description: 'Generate the application key' },
  { name: 'route-list', label: 'Routes', command: 'php artisan route:list', icon: 'list', description: 'List the registered routes' },
  { name: 'storage-link', label: 'Storage link', command: 'php artisan storage:link', icon: 'link', description: 'Symlink public/storage to storage/app/public' },
];
const SYMFONY_COMMANDS = [
  { name: 'migrate', label: 'Migrate', command: 'php bin/console doctrine:migrations:migrate', icon: 'database', description: 'Apply Doctrine migrations', confirm: true },
  { name: 'cache-clear', label: 'Clear cache', command: 'php bin/console cache:clear', icon: 'broom', description: 'Clear the Symfony cache' },
  { name: 'router', label: 'Routes', command: 'php bin/console debug:router', icon: 'list', description: 'List the configured routes' },
];
const WORDPRESS_COMMANDS = [
  { name: 'cache-flush', label: 'Flush cache', command: 'wp cache flush', icon: 'broom', description: 'Flush the object cache' },
  { name: 'plugin-list', label: 'Plugins', command: 'wp plugin list', icon: 'list', description: 'List installed plugins' },
  { name: 'core-update', label: 'Update core', command: 'wp core update', icon: 'arrow-up', description: 'Update WordPress core', confirm: true },
];

function frameworkOf(domain: string): string {
  return (sites.find((s) => s.domain === domain)?.framework as string) || '';
}
function commandsFor(domain: string): Array<Record<string, unknown>> {
  switch (frameworkOf(domain)) {
    case 'laravel': return LARAVEL_COMMANDS;
    case 'symfony': return SYMFONY_COMMANDS;
    case 'wordpress': return WORDPRESS_COMMANDS;
    default: return [];
  }
}

const DOCTOR_LARAVEL = {
  checks: [
    { name: 'app_key', label: 'Application key', status: 'ok' },
    { name: 'migrations', label: 'Migrations', status: 'warn', detail: '2 pending migrations', fix: 'migrate' },
    { name: 'env_drift', label: '.env drift', status: 'ok' },
    { name: 'storage_link', label: 'Storage link', status: 'ok' },
  ],
  failures: 0,
  warnings: 1,
};
const DOCTOR_OK = {
  checks: [{ name: 'serving', label: 'Serving over HTTPS', status: 'ok' }],
  failures: 0,
  warnings: 0,
};
function doctorFor(domain: string): Record<string, unknown> {
  return frameworkOf(domain) === 'laravel' ? DOCTOR_LARAVEL : DOCTOR_OK;
}

// Running a command card streams the same SSE contract the daemon emits.
function commandRunSSE(domain: string, name: string): Response {
  const cmd = commandsFor(domain).find((c) => c.name === name);
  const line = (cmd?.command as string) || name;
  const body =
    `event: stdout\ndata: $ ${line}\n\n` +
    `event: stdout\ndata: Running…\n\n` +
    `event: stdout\ndata: Done.\n\n` +
    `event: done\ndata: ${JSON.stringify({ exit: 0, durationMs: 640 })}\n\n`;
  return new Response(body, { status: 200, headers: { 'content-type': 'text/event-stream' } });
}


// ---- Per-site cron (Sites -> Cron) ----
// A schedule reads as fixture data only if the timestamps move, so the fixture
// stores offsets ("@+9h", "@-40s") and they are stamped relative to now here.
// Mutable, so adding, editing, deleting and the WordPress switch all behave the
// way they do against the daemon.
interface DemoCronRun {
  at: string;
  ok: boolean;
  running: boolean;
  exit_code: number;
  result?: string;
  output?: string[];
}
interface DemoCronEntry {
  id: string;
  name: string;
  command: string;
  schedule: string;
  calendar: string;
  capture_output: boolean;
  disabled: boolean;
  managed?: boolean;
  unit: string;
  next_run?: string;
  last_run?: DemoCronRun;
}
interface DemoSiteCron {
  supported: boolean;
  unsupported?: string;
  pseudo_cron: {
    available: boolean;
    label?: string;
    description?: string;
    replaced: boolean;
    schedule?: string;
    command?: string;
    constant?: string;
    file?: string;
  };
  entries: DemoCronEntry[];
}

type DemoBackupArchive = { name: string; size: number; taken: string };
type DemoSiteBackups = {
  schedule: string;
  verify: string;
  disabled: boolean;
  keep: { daily: number; weekly: number; monthly: number };
  key_path: string;
  archives: DemoBackupArchive[];
};
const backupsBySite = structuredClone(backupsFixture) as Record<string, DemoSiteBackups>;

// Alerts are mutable so dismissing one in the demo takes it off the card,
// which is the only way to see the empty state the dashboard hides.
interface DemoAlert {
  kind: string;
  site?: string;
  title: string;
  message: string;
  at: string;
}
let demoAlerts = (structuredClone(alertsFixture) as { alerts: DemoAlert[] }).alerts;

// The security page. Mutable so authorising and removing a key in the demo
// lands on the list, which is the only part of that page servlo really changes.
interface DemoSSHKey { type: string; comment: string; fingerprint: string }
const demoSecurity = structuredClone(securityFixture) as Record<string, unknown> & { keys: DemoSSHKey[] };

// Staging, per domain. Mutable so a refresh in the demo moves the timestamp.
interface DemoStaging {
  staging: boolean;
  origin?: string;
  origin_domain?: string;
  origin_exists: boolean;
  user?: string;
  refreshed_at?: string;
  copies: string[];
}
const demoStaging: Record<string, DemoStaging> = {
  'acme.test': { staging: false, origin_exists: false, copies: ['staging.acme-supply.com'] },
  'acme-supply.com': { staging: false, origin_exists: false, copies: ['staging.acme-supply.com'] },
  'staging.acme-supply.com': {
    staging: true,
    origin: 'acme-supply-com',
    origin_domain: 'acme-supply.com',
    origin_exists: true,
    user: 'staging',
    refreshed_at: '@-3h',
    copies: []
  }
};

function backupsFor(domain: string): DemoSiteBackups {
  if (!backupsBySite[domain]) {
    backupsBySite[domain] = structuredClone(backupsBySite['default']);
  }
  return backupsBySite[domain];
}

const cronBySite = structuredClone(cronFixture) as Record<string, DemoSiteCron>;

const OFFSET_UNITS: Record<string, number> = { s: 1000, m: 60_000, h: 3_600_000, d: 86_400_000 };

// "@+9h" / "@-40s" -> an ISO timestamp that many units either side of now.
function stampOffset(value: string): string {
  const match = /^@([+-])(\d+)([smhd])$/.exec(value);
  if (!match) return value;
  const delta = Number(match[2]) * OFFSET_UNITS[match[3]];
  return new Date(Date.now() + (match[1] === '-' ? -delta : delta)).toISOString();
}

function cronFor(domain: string): DemoSiteCron {
  const known = cronBySite[domain];
  if (known) return known;
  // Every other demo site has a schedule of its own to add to, rather than a
  // tab that looks broken.
  cronBySite[domain] = {
    supported: true,
    pseudo_cron: { available: false, replaced: false },
    entries: []
  };
  return cronBySite[domain];
}

function cronResponse(domain: string): DemoSiteCron {
  const site = cronFor(domain);
  return {
    ...site,
    entries: site.entries.map((e) => ({
      ...e,
      next_run: e.next_run ? stampOffset(e.next_run) : undefined,
      last_run: e.last_run ? { ...e.last_run, at: stampOffset(e.last_run.at) } : undefined
    }))
  };
}

// Enough of the real translation for the demo to show a saved entry's calendar:
// the two forms an operator types most, and the input itself for the rest.
function demoCalendar(schedule: string): string {
  const cron = schedule.trim().split(/\s+/);
  if (cron.length === 5) {
    const [min, hour] = cron;
    if (min.startsWith('*/')) return `*-*-* *:0/${min.slice(2)}:00`;
    if (min === '*' && hour === '*') return '*-*-* *:*:00';
    if (/^\d+$/.test(min) && /^\d+$/.test(hour))
      return `*-*-* ${hour.padStart(2, '0')}:${min.padStart(2, '0')}:00`;
  }
  return schedule.replace(/^@/, '');
}

function cronSlug(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'job';
}

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}
function textResponse(s: string): Response {
  return new Response(s, { status: 200, headers: { 'content-type': 'text/plain' } });
}

// Installing a preset streams newline-delimited JSON phase events ending in a
// `done`. Mark the preset installed and drop a minimal service into the live
// list so the picker and the services grid update like the real flow.
function presetInstallStream(name: string, version: string): Response {
  const preset = presets.find((p) => p.name === name);
  const image = (preset?.image as string) || `docker.io/library/${name}:latest`;
  if (preset) {
    preset.installed = true;
    if (version) preset.installed_tags = [...((preset.installed_tags as string[]) || []), version];
  }
  const svcName = version ? `${name}-${version}` : name;
  if (!services.some((s) => s.name === svcName)) {
    services.push({
      name: svcName,
      status: 'active',
      version: version || 'latest',
      env_vars: {},
      dashboard: (preset?.dashboard as string) || undefined,
      custom: true,
      preset_owned: true,
      site_count: 0,
      pinned: false,
      migration_supported: false,
      can_rollback: false,
    });
  }
  const body =
    `${JSON.stringify({ phase: 'pulling_image', image })}\n` +
    `${JSON.stringify({ phase: 'starting_unit' })}\n` +
    `${JSON.stringify({ phase: 'waiting_ready' })}\n` +
    `${JSON.stringify({ phase: 'done', name: svcName })}\n`;
  return new Response(body, { status: 200, headers: { 'content-type': 'application/x-ndjson' } });
}

// ---- File manager ----
// One tree, reused for every site: the point of the fixture is the shapes a
// listing can take (a nested folder, an empty one, a name long enough to
// truncate, a symlink that leaves the site, an archive worth extracting), not
// which domain it belongs to.
const fileListings = filesFixture.listings as Record<
  string,
  { entries: Array<Record<string, unknown>>; truncated: boolean }
>;
const fileContent = filesFixture.content as Record<string, Record<string, unknown>>;

// A .env with real-looking values, so the editor renders something rather than
// an empty buffer, and so the 0600 mode on the row has a reason behind it.
(fileContent['.env'] as Record<string, unknown>).text = ENV_TEXT;

function fileListingFor(at: string): unknown {
  const known = fileListings[at];
  return {
    path: at,
    root: filesFixture.root,
    entries: known ? known.entries : [],
    truncated: known ? known.truncated : false,
    ...(known ? {} : { error: '' }),
  };
}

const sftp = structuredClone(sftpFixture) as Record<string, unknown>;

const realFetch = window.fetch.bind(window);
window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
  let path = raw;
  let search = '';
  try {
    const u = new URL(raw, location.href);
    path = u.pathname;
    search = u.search;
  } catch {
    /* keep raw */
  }
  if (path.length > 1 && path.endsWith('/')) path = path.slice(0, -1);
  const method = (init?.method ?? 'GET').toUpperCase();
  const qs = new URLSearchParams(search);

  // Live (mutable) collections
  if (path === '/api/sites') return jsonResponse(sites);
  if (path === '/api/services') return jsonResponse(services);
  if (path === '/api/services/presets') return jsonResponse(presets);

  if (path === '/api/lan/status' && method === 'POST') {
    const body = JSON.parse(String(init?.body || '{}')) as { action?: string };
    if (body.action === 'expose' || body.action === 'unexpose') {
      lanStatus.exposed = body.action === 'expose';
      lanStatus.lan_ip = lanStatus.exposed ? '192.168.1.42' : '';
    } else if (body.action === 'services_on' || body.action === 'services_off') {
      lanStatus.services_enabled = body.action === 'services_on';
    }
    const result = {
      result: 'ok',
      exposed: lanStatus.exposed,
      services_enabled: lanStatus.services_enabled,
      services_reachable: lanStatus.exposed && lanStatus.services_enabled,
    };
    return new Response(`${JSON.stringify(result)}\n`, {
      status: 200,
      headers: { 'content-type': 'application/x-ndjson' }
    });
  }

  // An engine's databases. An engine with no fixture reports none rather than
  // falling through to the empty catch-all, which the tab reads as an error.
  const engine = path.match(/^\/api\/databases\/([^/]+)$/);
  if (engine && method === 'GET') {
    const name = decodeURIComponent(engine[1]);
    const known = (databasesFixture as Record<string, unknown>)[name];
    return jsonResponse(
      known ?? {
        service: name,
        family: '',
        status: 'active',
        supports_create: false,
        supports_snapshot: false,
        databases: [],
      },
    );
  }

  // Installing a preset streams progress; anything under presets/<name>.
  const presetInstall = path.match(/^\/api\/services\/presets\/([^/]+)$/);
  if (presetInstall && method === 'POST')
    return presetInstallStream(decodeURIComponent(presetInstall[1]), qs.get('version') || '');

  // The file manager. Listing, one file's content, the permission plan, and
  // the writes, which answer ok so the panel's success paths are reachable.
  const filesMatch = path.match(/^\/api\/sites\/([^/]+)\/files(?:\/([^/]+))?$/);
  if (filesMatch) {
    const leaf = filesMatch[2] ?? '';
    const at = qs.get('path') ?? '';
    if (!leaf && method === 'GET') return jsonResponse(fileListingFor(at));
    if (leaf === 'content' && method === 'GET') {
      const known = fileContent[at];
      return jsonResponse(
        known ?? { path: at, text: '', size: 0, mode: '0644', binary: false, truncated: false },
      );
    }
    if (leaf === 'content' && method === 'PUT') return jsonResponse({ ok: true, path: at });
    if (leaf === 'upload' && method === 'POST') return jsonResponse({ ok: true, path: at });
    if (leaf === 'unzip' && method === 'POST') return jsonResponse({ ok: true, files: 214, bytes: 8419233 });
    if (leaf === 'entry' && method === 'DELETE') return jsonResponse({ ok: true, path: at });
    if (leaf === 'permissions' && method === 'GET') return jsonResponse(filesFixture.permissions);
    if (leaf === 'permissions' && method === 'POST')
      return jsonResponse({ ...filesFixture.permissions, applied: 214, changes: 0 });
  }

  // SFTP access. Authorising and withdrawing mutate the fixture, so the page
  // behaves like the real one when it reloads itself after a write.
  if (path === '/api/sftp' && method === 'GET') return jsonResponse(sftp);
  if (path === '/api/sftp' && method === 'POST') {
    const body = JSON.parse(String(init?.body || '{}')) as {
      domain?: string;
      label?: string;
      key?: string;
    };
    const sites_ = sftp.sites as Array<Record<string, unknown>>;
    const site = sites_.find((s) => s.domain === body.domain);
    const key = {
      site: body.domain,
      label: body.label,
      type: (body.key ?? '').split(' ')[0] || 'ssh-ed25519',
      fingerprint: 'SHA256:demo' + Math.random().toString(36).slice(2, 12),
      site_path: '/home/dev/code/demo',
    };
    if (site) (site.keys as unknown[]).push(key);
    else
      sites_.push({
        domain: body.domain,
        path: '/home/dev/code/demo',
        port: 2200 + sites_.length,
        confined: false,
        keys: [key],
      });
    return jsonResponse({ ok: true, path: body.domain });
  }
  const sftpKeyMatch = path.match(/^\/api\/sftp\/keys\/(.+)$/);
  if (sftpKeyMatch && method === 'DELETE') {
    const fingerprint = decodeURIComponent(sftpKeyMatch[1]);
    const sites_ = sftp.sites as Array<Record<string, unknown>>;
    for (const site of sites_) {
      site.keys = (site.keys as Array<Record<string, unknown>>).filter(
        (k) => k.fingerprint !== fingerprint,
      );
    }
    sftp.sites = sites_.filter((s) => (s.keys as unknown[]).length > 0);
    return jsonResponse({ ok: true });
  }

  // Per-site request-timing analytics — a busy profile with flagged slow routes
  // and a cold start for a couple of sites, a healthy populated one for the rest.
  const analyticsMatch = path.match(/^\/api\/sites\/([^/]+)\/analytics$/);
  if (analyticsMatch) {
    const domain = decodeURIComponent(analyticsMatch[1]);
    return jsonResponse(analyticsFor(domain, qs.get('range') || '1h'));
  }

  // Per-site database account (the Settings tab's Database account card). A
  // site with no fixture of its own gets the ordinary case, so every site in
  // the demo renders the card rather than half of them showing nothing.
  const dbUserMatch = path.match(/^\/api\/sites\/([^/]+)\/db-user$/);
  if (dbUserMatch && method === 'GET') {
    const known = dbUsers as Record<string, unknown>;
    const domain = decodeURIComponent(dbUserMatch[1]);
    return jsonResponse(known[domain] ?? known['default']);
  }
  // The panel's own mail account (System → Mail), separate from any site's.
  if (path === '/api/settings/smtp/test' && method === 'POST') {
    if (!smtp.panel.configured) return jsonResponse({ error: 'the panel has no SMTP settings yet' }, 400);
    const to = String((JSON.parse(String(init?.body || '{}')) as { to?: string }).to || '');
    return jsonResponse({ ok: true, to: to || String(smtp.panel.settings.from_address || '') });
  }
  if (path === '/api/settings/smtp') {
    if (method === 'GET') return jsonResponse(smtp.panel);
    if (method === 'POST') {
      applySMTP(smtp.panel, String(init?.body || '{}'));
      return jsonResponse({ ok: true });
    }
    if (method === 'DELETE') {
      smtp.panel.configured = false;
      smtp.panel.settings = { has_password: false };
      return jsonResponse({ ok: true });
    }
  }

  // Per-site outgoing mail (Settings tab → Outgoing mail). Save, test and
  // remove all mutate the in-memory copy so the card reflects what was pressed.
  const smtpTest = path.match(/^\/api\/sites\/([^/]+)\/smtp\/test$/);
  if (smtpTest && method === 'POST') {
    const account = smtpFor(decodeURIComponent(smtpTest[1]));
    const to = String((JSON.parse(String(init?.body || '{}')) as { to?: string }).to || '');
    if (!account.configured) return jsonResponse({ error: 'this site has no SMTP settings yet' }, 400);
    // Empty box falls back to the account's own sender address, as the API does.
    // One site refuses, so the failure path is visible in the demo too.
    if (String(account.settings.host || '').includes('mailgun'))
      return jsonResponse(
        { error: 'the mail server refused the sender orders@acme-supply.com: 550 5.7.1 Sender address not verified' },
        502,
      );
    return jsonResponse({ ok: true, to: to || String(account.settings.from_address || '') });
  }
  const smtpSite = path.match(/^\/api\/sites\/([^/]+)\/smtp$/);
  if (smtpSite) {
    const domain = decodeURIComponent(smtpSite[1]);
    const account = smtpFor(domain);
    if (method === 'GET') return jsonResponse(account);
    if (method === 'POST') {
      applySMTP(account, String(init?.body || '{}'));
      return jsonResponse({ ok: true, env_keys: account.env_keys ?? [] });
    }
    if (method === 'DELETE') {
      account.configured = false;
      account.settings = { has_password: false };
      return jsonResponse({ ok: true });
    }
  }

  const dbUserRotate = path.match(/^\/api\/sites\/([^/]+)\/db-user\/rotate$/);
  if (dbUserRotate && method === 'POST') {
    const known = dbUsers as Record<string, { user?: string; env_keys?: string[] }>;
    const account = known[decodeURIComponent(dbUserRotate[1])] ?? known['default'];
    return jsonResponse({ ok: true, user: account.user, env_keys: account.env_keys ?? [] });
  }

  // Per-site application logs (Logs tab → App logs). List files, then a file's
  // entries; the /clear POST falls through to {ok:true} like other actions.
  const appLogsMatch = path.match(/^\/api\/app-logs\/([^/]+)(?:\/([^/]+))?$/);
  if (appLogsMatch && method === 'GET') {
    const file = appLogsMatch[2];
    if (!file) return jsonResponse({ files: APP_LOG_FILES });
    if (file !== 'clear') return jsonResponse({ entries: appLogEntries() });
  }

  // Per-site .env editor — GET reads only; saves/restores fall through to {ok:true}
  if (method === 'GET') {
    if (/\/env\/files$/.test(path)) return jsonResponse(['.env', '.env.example']);
    if (/\/env\/backups\/.+/.test(path)) return textResponse(ENV_TEXT);
    if (/\/env\/backups$/.test(path)) return jsonResponse([]);
    if (/\/env$/.test(path)) return textResponse(ENV_TEXT);
  }

  // Nginx — global /api/nginx and per-site /api/sites/<domain>/nginx (+ /backups)
  if (path.includes('/nginx')) {
    if (path.includes('/backups')) return /\/backups\/.+/.test(path) ? textResponse(NGINX_TEXT) : jsonResponse([]);
    if (method === 'GET') return jsonResponse({ path: '/home/dev/.config/servlo/nginx/acme-supply.com.conf', content: NGINX_TEXT, exists: true });
    return jsonResponse({ ok: true, content: NGINX_TEXT, exists: true });
  }
  // php.ini config (per PHP version, or per-site for FrankenPHP) — GET reads only
  if (method === 'GET' && /\/php-versions\/[^/]+\/config$/.test(path))
    return jsonResponse({ path: '~/.config/servlo/php/8.4/php.ini', content: PHP_INI_TEXT, exists: true });


  // Staging: the two sides of the same relationship. A live site with one copy,
  // and the copy itself.
  const stagingMatch = path.match(/^\/api\/sites\/([^/]+)\/staging$/);
  if (stagingMatch) {
    const domain = decodeURIComponent(stagingMatch[1]);
    const state = demoStaging[domain];
    if (method === 'POST') {
      const body = JSON.parse(String(init?.body ?? '{}')) as { action?: string; bring?: string };
      if (!state?.staging) return jsonResponse({ ok: false, error: domain + ' is not a staging site' });
      if (body.action === 'password') {
        return jsonResponse({ ok: true, user: state.user, password: 'kQ7pR2wX9mL4vN8bT6yH3sD5fG1jZ0aC' });
      }
      state.refreshed_at = new Date().toISOString();
      return jsonResponse({
        ok: true,
        files: 4127,
        bytes: 488000000,
        database: body.bring === 'files' ? '' : 'staging_acme'
      });
    }
    return jsonResponse(
      state ? { ...state, refreshed_at: state.refreshed_at ? stampOffset(state.refreshed_at) : '' }
            : { staging: false, origin_exists: false, copies: [] }
    );
  }

  if (path === '/api/security') return jsonResponse(demoSecurity);
  if (path === '/api/security/keys') {
    const body = JSON.parse(String(init?.body ?? '{}')) as {
      action?: string; key?: string; name?: string; fingerprint?: string;
    };
    if (body.action === 'remove') {
      demoSecurity.keys = demoSecurity.keys.filter((k) => k.fingerprint !== body.fingerprint);
      return jsonResponse({ ok: true, keys: demoSecurity.keys });
    }
    const line = String(body.key ?? '').trim();
    const name = String(body.name ?? '').trim();
    if (!line.startsWith('ssh-')) {
      return jsonResponse({
        keys: demoSecurity.keys,
        error:
          'that is not a public key servlo recognises. It should start with a type such as ssh-ed25519 followed by the key itself'
      });
    }
    demoSecurity.keys = [
      ...demoSecurity.keys,
      { type: line.split(' ')[0], comment: name, fingerprint: 'SHA256:' + line.slice(-43) }
    ];
    return jsonResponse({ ok: true, keys: demoSecurity.keys });
  }

  // Servlo's own state: what a rebuild onto a fresh machine needs.
  if (path === '/api/backup/state') {
    if (method === 'POST') {
      return jsonResponse({ ok: true, name: 'servlo-state-20260309-141500.servlobak', size: 185344, files: 37 });
    }
    return jsonResponse(serverStateFixture);
  }

  // What the app store offers, for the Add Site form's fourth source.
  if (path === '/api/apps') {
    return jsonResponse(appsFixture);
  }

  // Installing one. WordPress is the app that ends with an account, so this
  // answers the way that one does: credentials the panel shows once.
  if (path === '/api/sites/app') {
    return jsonResponse({
      ok: true,
      site: 'blog-acme-com',
      domain: 'blog.acme.com',
      path: '/home/servlo/sites/blog.acme.com',
      admin_user: 'admin',
      admin_password: 'Qr7-tvB2xk9WmLpc',
      database: 'blog_acme_com'
    });
  }

  // What is currently wrong with the server, and dismissing one of them.
  if (path === '/api/alerts') {
    if (method === 'POST') {
      const body = JSON.parse(String(init?.body ?? '{}')) as { kind?: string; site?: string };
      if (!body.kind) return jsonResponse({ alerts: [], error: 'which alert?' });
      demoAlerts = demoAlerts.filter((a) => !(a.kind === body.kind && (a.site ?? '') === (body.site ?? '')));
    }
    return jsonResponse({ alerts: demoAlerts.map((a) => ({ ...a, at: stampOffset(a.at) })) });
  }

  // Per-site backups: what is scheduled, what is on disk, and the four things
  // the card can do. Above the catch-alls for the same reason cron is.
  const backupList = path.match(/^\/api\/sites\/([^/]+)\/backups$/);
  if (backupList) {
    const domain = decodeURIComponent(backupList[1]);
    const state = backupsFor(domain);
    if (method === 'GET') {
      return jsonResponse({
        ...state,
        archives: state.archives.map((a) => ({ ...a, taken: stampOffset(a.taken) }))
      });
    }
    const body = JSON.parse(String(init?.body ?? '{}'));
    if (body.action === 'run') {
      const name = `${domain.split('.')[0]}-${new Date().toISOString().slice(0, 10).replace(/-/g, '')}-041500.servlobak`;
      state.archives.unshift({ name, size: 488000000, taken: '@-1s' });
      return jsonResponse({ ok: true, archive: name, files: 4127, pruned: 1 });
    }
    if (body.action === 'verify') {
      return jsonResponse({ ok: true, archive: state.archives[0]?.name ?? '', tables: 63 });
    }
    if (body.action === 'unschedule') {
      state.schedule = '';
      state.verify = '';
      return jsonResponse({ ok: true });
    }
    state.schedule = body.schedule === 'daily' ? 'daily' : '*-*-* 03:30:00';
    state.verify = body.verify ? 'Sun *-*-* 04:00:00' : '';
    if (body.keep) state.keep = body.keep;
    return jsonResponse({ ok: true });
  }

  // Per-site cron: the schedule, saving one, deleting one, and the framework's
  // pseudo-cron switch. Above the catch-alls, which would otherwise answer the
  // DELETE with a bare {ok:true} and leave the row on screen.
  const cronList = path.match(/^\/api\/sites\/([^/]+)\/cron$/);
  if (cronList) {
    const domain = decodeURIComponent(cronList[1]);
    if (method === 'GET') return jsonResponse(cronResponse(domain));
    if (method === 'POST') {
      const body = JSON.parse(String(init?.body || '{}')) as Partial<DemoCronEntry> & { id?: string };
      const site = cronFor(domain);
      const id = body.id || cronSlug(String(body.name || ''));
      const entry: DemoCronEntry = {
        id,
        name: String(body.name || ''),
        command: String(body.command || ''),
        schedule: String(body.schedule || ''),
        calendar: demoCalendar(String(body.schedule || '')),
        capture_output: Boolean(body.capture_output),
        disabled: Boolean(body.disabled),
        unit: `servlo-cron-${domain.split('.')[0]}-${id}`
      };
      const at = site.entries.findIndex((e) => e.id === id);
      if (at >= 0) entry.last_run = site.entries[at].last_run;
      if (at >= 0) site.entries[at] = entry;
      else site.entries.push(entry);
      return jsonResponse({ ok: true, entry });
    }
  }
  const cronOne = path.match(/^\/api\/sites\/([^/]+)\/cron\/([^/]+)$/);
  if (cronOne && method === 'DELETE') {
    const site = cronFor(decodeURIComponent(cronOne[1]));
    const id = decodeURIComponent(cronOne[2]);
    site.entries = site.entries.filter((e) => e.id !== id);
    return jsonResponse({ ok: true });
  }
  const pseudoCron = path.match(/^\/api\/sites\/([^/]+)\/pseudo-cron$/);
  if (pseudoCron && method === 'POST') {
    const site = cronFor(decodeURIComponent(pseudoCron[1]));
    const replace = Boolean((JSON.parse(String(init?.body || '{}')) as { replace?: boolean }).replace);
    site.pseudo_cron.replaced = replace;
    if (replace) {
      site.entries = [
        {
          id: 'wp-cron',
          name: site.pseudo_cron.label || 'Framework cron',
          command: site.pseudo_cron.command || '',
          schedule: site.pseudo_cron.schedule || '',
          calendar: demoCalendar(site.pseudo_cron.schedule || ''),
          capture_output: false,
          disabled: false,
          managed: true,
          unit: 'servlo-cron-blog-orbitlabs-wp-cron',
          next_run: '@+1m'
        },
        ...site.entries.filter((e) => !e.managed)
      ];
    } else {
      site.entries = site.entries.filter((e) => !e.managed);
    }
    return jsonResponse({ ok: true });
  }

  // Overview "Actions": command list, doctor report, and running a command.
  const cmdRun = path.match(/^\/api\/sites\/([^/]+)\/commands\/([^/]+)\/run$/);
  if (cmdRun && method === 'POST')
    return commandRunSSE(decodeURIComponent(cmdRun[1]), decodeURIComponent(cmdRun[2]));
  const cmdList = path.match(/^\/api\/sites\/([^/]+)\/commands$/);
  if (cmdList) return jsonResponse({ commands: commandsFor(decodeURIComponent(cmdList[1])) });
  const doctorMatch = path.match(/^\/api\/sites\/([^/]+)\/doctor$/);
  if (doctorMatch) return jsonResponse(doctorFor(decodeURIComponent(doctorMatch[1])));

  // Managed host tools. The check re-reads the pins, which in the demo are the
  // ones already loaded, and an update applies the pin so the card that asked
  // lands on its up-to-date state instead of staying flagged.
  const tools = () => (status.tools ?? []) as Array<Record<string, unknown>>;
  if (path === '/api/tools/check' && method === 'POST')
    return jsonResponse({ ok: true, tools: tools() });
  const toolUpdate = path.match(/^\/api\/tools\/([^/]+)\/update$/);
  if (toolUpdate && method === 'POST') {
    const tool = tools().find((t) => t.name === decodeURIComponent(toolUpdate[1]));
    if (tool) {
      tool.installed = tool.pinned;
      tool.update_available = false;
    }
    return jsonResponse({ ok: true });
  }

  // Database connections. Testing one is the action worth stubbing: a managed
  // database whose trusted sources have not been set up yet is exactly the
  // state the card has to render, and it is invisible from a happy fixture.
  if (path === '/api/db-connections' && method === 'POST') {
    const body = JSON.parse(String(init?.body || '{}')) as { action?: string; name?: string };
    if (body.action === 'test' && body.name === 'analytics') {
      return jsonResponse({
        error:
          'cannot reach analytics-mysql-do-user-1234567-0.k.db.ondigitalocean.com:25060: dial tcp 10.20.30.40:25060: i/o timeout. ' +
          "If this is a managed database, add this server's public IP to the provider's trusted sources and check the port",
      });
    }
    if (body.action === 'test') return jsonResponse({ ok: true });
    return jsonResponse({ ok: true, connections: dbConnections });
  }

  // Static fixtures
  if (path in ROUTES) return jsonResponse(ROUTES[path]);

  // Toggles / actions: pretend they succeeded so the UI flips optimistically.
  if (method !== 'GET' && method !== 'HEAD') return jsonResponse({ ok: true });
  // Anything else we didn't fixture (push keys, favicons, …) — harmless empty.
  if (path.startsWith('/api/')) return jsonResponse({});
  return realFetch(input as RequestInfo, init);
};

// External opens (a site's own URL, a service dashboard) have no server in the
// demo, so they go to a styled mockup page rather than a dead tab. Links to the
// project itself open for real.
const realOpen = window.open.bind(window);
(window as unknown as { open: typeof window.open }).open = ((
  url?: string | URL,
  target?: string,
  features?: string
) => {
  const u = String(url ?? '');
  if (/github\.com\/realrashid/.test(u) || u === '' || u.startsWith('#')) return realOpen(url, target, features);
  let host = u;
  try {
    host = new URL(u, location.href).host || u;
  } catch {
    /* keep u */
  }
  return realOpen(
    `preview.html?host=${encodeURIComponent(host)}&url=${encodeURIComponent(u)}`,
    '_blank',
    'noopener'
  );
}) as typeof window.open;

// Canned log lines for the Logs tab's streamed views (FPM/container, queue,
// schedule, reverb, and the host dev-server journal), picked off the stream path
// so each sub-tab shows realistic output instead of a forever-connecting empty
// viewer. Timestamps are stamped relative to now so the tail reads as live.
function logLinesFor(url: string): string[] {
  const now = Date.now();
  const t = (secAgo: number) => new Date(now - secAgo * 1000).toISOString().replace('T', ' ').slice(0, 19);
  if (/\/queue\//.test(url) || /\/horizon\//.test(url))
    return [
      `[${t(52)}] Processing: App\\Jobs\\SendShipmentNotification`,
      `[${t(52)}] Processed:  App\\Jobs\\SendShipmentNotification`,
      `[${t(30)}] Processing: App\\Jobs\\ChargeCard`,
      `[${t(29)}] Processed:  App\\Jobs\\ChargeCard`,
      `[${t(8)}] Processing: App\\Jobs\\RebuildSearchIndex`,
    ];
  if (/\/schedule\//.test(url))
    return [
      `[${t(120)}] Running scheduled command: php artisan telescope:prune --hours=48`,
      `[${t(120)}] Command "telescope:prune" ran successfully.`,
      `[${t(60)}] Running scheduled command: php artisan sitemap:generate`,
      `[${t(60)}] Command "sitemap:generate" ran successfully.`,
    ];
  if (/\/reverb\//.test(url))
    return [
      `${t(40)}  Connection 8f2ac1 established`,
      `${t(38)}  Subscribed to channel: orders.42`,
      `${t(12)}  Broadcasting App\\Events\\OrderShipped on orders.42`,
    ];
  if (/\/worker\//.test(url))
    return [
      `  VITE v5.4.10  ready in 214 ms`,
      `  ➜  Local:   https://acme-supply.com:5173/`,
      `  ➜  press h + enter to show help`,
      `${t(6)} [vite] hmr update /resources/js/app.js`,
    ];
  // FPM / container: an access + php-error mix (the default runtime tab)
  return [
    `[${t(18)}] 127.0.0.1  "GET /"  200  138ms`,
    `[${t(15)}] 127.0.0.1  "GET /products"  200  70ms`,
    `[${t(9)}] 127.0.0.1  "POST /checkout"  302  199ms`,
    `[${t(6)}] PHP Warning:  Undefined array key "coupon" in /app/Http/Controllers/CheckoutController.php on line 52`,
    `[${t(4)}] 127.0.0.1  "GET /orders/42"  200  233ms`,
  ];
}

// EventSource that "connects" then replays the canned debug events or log lines.
class DemoEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  private listeners: Record<string, Array<(ev: unknown) => void>> = {};

  constructor(url: string) {
    const u = String(url);
    const isLog = /\/logs(\/|$|\?)/.test(u);
    setTimeout(() => {
      this.readyState = 1;
      this.emit('open', { type: 'open' });
      if (isLog) {
        for (const line of logLinesFor(u)) this.emit('message', { data: line });
      }
    }, 0);
  }
  addEventListener(type: string, cb: (ev: unknown) => void) {
    (this.listeners[type] ||= []).push(cb);
  }
  removeEventListener(type: string, cb: (ev: unknown) => void) {
    this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== cb);
  }
  private emit(type: string, ev: unknown) {
    (this as unknown as Record<string, ((e: unknown) => void) | null>)['on' + type]?.(ev);
    (this.listeners[type] || []).forEach((cb) => cb(ev));
  }
  close() {
    this.readyState = 2;
  }
}
(window as unknown as { EventSource: unknown }).EventSource = DemoEventSource;

// A WebSocket that "connects" and then stays quiet — no live pushes, no
// reconnect storm. The UI shows itself as connected and runs off the fixtures.
class DemoWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: unknown) => void) | null = null;
  onclose: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  private listeners: Record<string, Array<(ev: unknown) => void>> = {};

  constructor() {
    setTimeout(() => {
      this.readyState = 1;
      const ev = { type: 'open' };
      this.onopen?.(ev);
      (this.listeners['open'] || []).forEach((cb) => cb(ev));
    }, 0);
  }
  addEventListener(type: string, cb: (ev: unknown) => void) {
    (this.listeners[type] ||= []).push(cb);
  }
  removeEventListener(type: string, cb: (ev: unknown) => void) {
    this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== cb);
  }
  send() {
    /* no-op */
  }
  close() {
    this.readyState = 3;
  }
}
(window as unknown as { WebSocket: unknown }).WebSocket = DemoWebSocket;

// The app registers a service worker in main.ts; the demo skips that entirely.
