import { describe, it, expect } from 'vitest';
import { categoryOf, groupByCategory, CATEGORY_ORDER, CATEGORY_LABELS } from './presetCategories';
import type { Preset } from '$stores/presets';

function p(name: string): Preset {
  return { name } as Preset;
}

describe('categoryOf', () => {
  it('maps known presets to their category', () => {
    expect(categoryOf('mysql')).toBe('databases');
    expect(categoryOf('redis')).toBe('cache');
    expect(categoryOf('typesense')).toBe('search');
    expect(categoryOf('mailpit')).toBe('mail');
    expect(categoryOf('pgadmin')).toBe('admin');
    expect(categoryOf('rustfs')).toBe('storage');
    expect(categoryOf('selenium')).toBe('testing');
  });

  it('maps the newer presets that landed on main', () => {
    expect(categoryOf('opensearch')).toBe('search');
    expect(categoryOf('redisinsight')).toBe('admin');
    expect(categoryOf('beanstalkd')).toBe('messaging');
    expect(categoryOf('soketi')).toBe('messaging');
    // rabbitmq moved out of cache into the messaging bucket
    expect(categoryOf('rabbitmq')).toBe('messaging');
  });

  it('falls back to the family prefix for versioned variants', () => {
    expect(categoryOf('postgres-17')).toBe('databases');
    expect(categoryOf('mysql-5-7')).toBe('databases');
  });

  it('returns other for unknown presets', () => {
    expect(categoryOf('totally-new-thing')).toBe('other');
  });
});

describe('groupByCategory', () => {
  it('groups presets and only returns non-empty categories in order', () => {
    const groups = groupByCategory([p('redis'), p('mysql'), p('mongo'), p('mailpit')]);
    expect(groups.map((g) => g.key)).toEqual(['databases', 'cache', 'mail']);
    expect(groups[0].presets.map((x) => x.name)).toEqual(['mongo', 'mysql']);
  });

  it('sorts presets alphabetically within a category', () => {
    const groups = groupByCategory([p('valkey'), p('memcached'), p('redis')]);
    expect(groups[0].presets.map((x) => x.name)).toEqual(['memcached', 'redis', 'valkey']);
  });

  it('keeps the global category order regardless of input order', () => {
    const groups = groupByCategory([p('selenium'), p('mysql')]);
    const idxDb = CATEGORY_ORDER.indexOf('databases');
    const idxTest = CATEGORY_ORDER.indexOf('testing');
    expect(idxDb).toBeLessThan(idxTest);
    expect(groups.map((g) => g.key)).toEqual(['databases', 'testing']);
  });

  it('orders the messaging bucket between cache and search', () => {
    const groups = groupByCategory([p('opensearch'), p('soketi'), p('redis')]);
    expect(groups.map((g) => g.key)).toEqual(['cache', 'messaging', 'search']);
  });

  it('returns an empty list for no presets', () => {
    expect(groupByCategory([])).toEqual([]);
  });
});

describe('CATEGORY_LABELS', () => {
  it('has a non-empty label for every category in CATEGORY_ORDER', () => {
    for (const key of CATEGORY_ORDER) {
      expect(typeof CATEGORY_LABELS[key]).toBe('function');
      expect(CATEGORY_LABELS[key]().length).toBeGreaterThan(0);
    }
  });
});
