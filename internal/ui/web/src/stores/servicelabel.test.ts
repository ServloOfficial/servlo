import { describe, it, expect } from 'vitest';
import { serviceLabel, serviceLabelOverrides } from './services';

// Every service servlo ships has to be spelled the way its makers spell it.
//
// serviceLabel falls back to capitalising the slug, which is right for Redis and
// Soketi and wrong for MariaDB, ClickHouse, OpenSearch and RedisInsight. The
// panel showed "Mariadb" in the rail and on the card, next to MySQL and
// PostgreSQL spelled correctly, because the override table listed eighteen
// services and the stores ship twenty-six. A fallback nobody sees is fine; one
// that decides how a shipped preset is named is a typo with a schedule.
//
// The names come from the stores rather than a list here, so adding a preset
// without deciding how it is spelled fails rather than shipping a guess. Only
// the paths are read, never the files.
const presetFiles = {
  ...import.meta.glob('../../../../config/presets/*.yaml'),
  ...import.meta.glob('../../../../../stores/services/*.yaml')
};

const shipped = Object.keys(presetFiles)
  .map((path) => path.replace(/^.*\//, '').replace(/\.yaml$/, ''))
  .sort();

describe('serviceLabel', () => {
  it('names every preset servlo ships', () => {
    expect(shipped.filter((name) => !(name in serviceLabelOverrides))).toEqual([]);
  });

  it('found the presets at all', () => {
    expect(shipped.length).toBeGreaterThan(20);
  });

  it('still capitalizes a service it has never heard of', () => {
    expect(serviceLabel('some-vendor-thing')).toBe('Some Vendor Thing');
  });
});
