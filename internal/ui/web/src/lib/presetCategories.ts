import type { Preset } from '$stores/presets';
import { m } from '../paraglide/messages.js';

export type CategoryKey =
  | 'databases'
  | 'cache'
  | 'messaging'
  | 'search'
  | 'mail'
  | 'admin'
  | 'storage'
  | 'testing'
  | 'other';

// Display order for the discovery sections.
export const CATEGORY_ORDER: CategoryKey[] = [
  'databases',
  'cache',
  'messaging',
  'search',
  'mail',
  'admin',
  'storage',
  'testing',
  'other'
];

const BY_NAME: Record<string, CategoryKey> = {
  mysql: 'databases',
  mariadb: 'databases',
  postgres: 'databases',
  'postgres-pgvector': 'databases',
  'postgres-postgis': 'databases',
  mongo: 'databases',
  redis: 'cache',
  valkey: 'cache',
  memcached: 'cache',
  rabbitmq: 'messaging',
  beanstalkd: 'messaging',
  soketi: 'messaging',
  elasticsearch: 'search',
  opensearch: 'search',
  meilisearch: 'search',
  typesense: 'search',
  mailpit: 'mail',
  gotenberg: 'mail',
  phpmyadmin: 'admin',
  pgadmin: 'admin',
  'mongo-express': 'admin',
  redisinsight: 'admin',
  elasticvue: 'admin',
  'typesense-dashboard': 'admin',
  rustfs: 'storage',
  selenium: 'testing',
  'stripe-mock': 'testing'
};

// Display label per category. Record<CategoryKey, ...> makes a missing entry a
// compile error, so adding a category can't silently fall back to "Other".
export const CATEGORY_LABELS: Record<CategoryKey, () => string> = {
  databases: m.services_cat_databases,
  cache: m.services_cat_cache,
  messaging: m.services_cat_messaging,
  search: m.services_cat_search,
  mail: m.services_cat_mail,
  admin: m.services_cat_admin,
  storage: m.services_cat_storage,
  testing: m.services_cat_testing,
  other: m.services_cat_other
};

export function categoryOf(name: string): CategoryKey {
  if (BY_NAME[name]) return BY_NAME[name];
  // Fall back to the family prefix so versioned variants like "postgres-17"
  // land in the same bucket as their base preset.
  const fam = name.match(/^([a-z][a-z0-9]*)/);
  if (fam && BY_NAME[fam[1]]) return BY_NAME[fam[1]];
  return 'other';
}

export interface CategoryGroup {
  key: CategoryKey;
  presets: Preset[];
}

export function groupByCategory(presets: Preset[]): CategoryGroup[] {
  const buckets = new Map<CategoryKey, Preset[]>();
  for (const p of presets) {
    const k = categoryOf(p.name);
    const arr = buckets.get(k) || [];
    arr.push(p);
    buckets.set(k, arr);
  }
  return CATEGORY_ORDER.filter((k) => buckets.has(k)).map((k) => ({
    key: k,
    presets: buckets.get(k)!.slice().sort((a, b) => a.name.localeCompare(b.name))
  }));
}
