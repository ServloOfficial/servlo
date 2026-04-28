import { writable, derived, get } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';
import { wsMessage } from '$lib/ws';

export interface Service {
  name: string;
  status: string;
  version?: string;
  env_vars?: Record<string, string>;
  dashboard?: string;
  dashboard_external?: boolean;
  connection_url?: string;
  custom?: boolean;
  site_count: number;
  site_domains?: string[];
  pinned?: boolean;
  paused?: boolean;
  depends_on?: string[];
  queue_site?: string;
  stripe_listener_site?: string;
  schedule_worker_site?: string;
  reverb_site?: string;
  horizon_site?: string;
  worker_site?: string;
  worker_name?: string;
}

export const services = writable<Service[]>([]);
export const servicesLoaded = writable<boolean>(false);

export async function loadServices() {
  try {
    const list = await apiJson<Service[]>('/api/services');
    services.set(Array.isArray(list) ? list : []);
    servicesLoaded.set(true);
  } catch {
    /* keep previous */
  }
}

export function applyServices(data: unknown) {
  if (!Array.isArray(data)) return;
  services.set(data as Service[]);
  servicesLoaded.set(true);
}

wsMessage.subscribe((msg) => {
  if (msg?.services) applyServices(msg.services);
});

function isWorker(s: Service): boolean {
  return Boolean(
    s.queue_site ||
      s.horizon_site ||
      s.stripe_listener_site ||
      s.schedule_worker_site ||
      s.reverb_site ||
      s.worker_site
  );
}

export const coreServices = derived(services, ($s) => $s.filter((x) => !isWorker(x)));

export interface WorkerGroup {
  key: string;
  label: string;
  items: Service[];
}

export const workerGroups = derived(services, ($s): WorkerGroup[] => {
  // The backend emits Laravel Horizon under both the `horizon_site` lens and
  // the generic `worker_site` lens with `worker_name=horizon`, which would
  // double up in the sidebar. Drop the framework-worker copy for 'horizon'.
  const workers = $s.filter(
    (x) => x.worker_site && !(x.worker_name === 'horizon' || x.name?.startsWith('horizon-'))
  );
  const groups: WorkerGroup[] = [
    { key: 'queue', label: 'Queues', items: $s.filter((x) => x.queue_site) },
    { key: 'horizon', label: 'Horizon', items: $s.filter((x) => x.horizon_site) },
    { key: 'schedule', label: 'Schedules', items: $s.filter((x) => x.schedule_worker_site) },
    { key: 'reverb', label: 'Reverb', items: $s.filter((x) => x.reverb_site) },
    { key: 'stripe', label: 'Stripe', items: $s.filter((x) => x.stripe_listener_site) },
    { key: 'workers', label: 'Workers', items: workers }
  ];
  return groups.filter((g) => g.items.length > 0);
});

export type ServiceAction = 'start' | 'stop' | 'restart' | 'pin' | 'unpin' | 'remove';

export async function serviceAction(name: string, action: ServiceAction): Promise<boolean> {
  try {
    const res = await apiFetch('/api/services/' + encodeURIComponent(name) + '/' + action, {
      method: 'POST'
    });
    if (res.ok) await loadServices();
    return res.ok;
  } catch {
    return false;
  }
}

export function findService(name: string): Service | undefined {
  return get(services).find((s) => s.name === name);
}

export function serviceLabel(name: string): string {
  const overrides: Record<string, string> = {
    phpmyadmin: 'phpMyAdmin',
    pgadmin: 'pgAdmin',
    mysql: 'MySQL',
    postgres: 'PostgreSQL',
    meilisearch: 'Meilisearch',
    mailpit: 'Mailpit',
    rustfs: 'RustFS',
    mongo: 'MongoDB',
    'mongo-express': 'Mongo Express',
    'stripe-mock': 'Stripe Mock',
    elasticsearch: 'Elasticsearch',
    elasticvue: 'Elasticvue',
    memcached: 'Memcached',
    rabbitmq: 'RabbitMQ'
  };
  if (overrides[name]) return overrides[name];
  // Versioned variants like "mysql-5-7"; show the family label.
  const m = name.match(/^([a-z][a-z0-9]*?)-(\d[\w-]*)$/);
  if (m && overrides[m[1]]) return overrides[m[1]];
  if (m) return capitalize(m[1]);
  return capitalize(name);
}

function capitalize(s: string): string {
  return s
    .split('-')
    .map((w) => (w.length ? w[0].toUpperCase() + w.slice(1) : w))
    .join(' ');
}

export function workerSiteName(s: Service): string {
  return (
    s.queue_site ||
    s.horizon_site ||
    s.schedule_worker_site ||
    s.reverb_site ||
    s.stripe_listener_site ||
    s.worker_site ||
    s.name
  );
}

export function parentSiteDomain(s: Service): string | null {
  const n =
    s.queue_site ||
    s.horizon_site ||
    s.schedule_worker_site ||
    s.reverb_site ||
    s.stripe_listener_site ||
    s.worker_site;
  return n ? n + '.test' : null;
}

export function detailLabel(s: Service): string {
  if (s.queue_site) return 'Queue worker';
  if (s.horizon_site) return 'Horizon';
  if (s.stripe_listener_site) return 'Stripe listener';
  if (s.schedule_worker_site) return 'Scheduler';
  if (s.reverb_site) return 'Reverb';
  if (s.worker_site && s.worker_name) return s.worker_name + ' worker';
  return serviceLabel(s.name);
}

export function isServiceWorker(s: Service): boolean {
  return isWorker(s);
}
