import { render } from '@testing-library/svelte';
import { describe, it, expect, beforeEach } from 'vitest';

// The children perform I/O on mount; replace them with stubs so these tests
// stay focused on tab visibility.
import { vi } from 'vitest';
vi.mock('./ServiceHeader.svelte', () => import('./ServiceDetail.stub.svelte'));
vi.mock('./ServiceSiteBadges.svelte', () => import('./ServiceDetail.stub.svelte'));
vi.mock('./PresetSuggestionBanner.svelte', () => import('./ServiceDetail.stub.svelte'));
vi.mock('./ServiceDatabasesTab.svelte', () => import('./ServiceDetail.stub.svelte'));
vi.mock('./ServiceEntitiesTab.svelte', () => import('./ServiceDetail.stub.svelte'));
vi.mock('$components/LogViewer.svelte', () => import('./ServiceDetail.stub.svelte'));

import ServiceDetail from './ServiceDetail.svelte';
import { session } from '$stores/session';
import type { Service } from '$stores/services';

function dbService(): Service {
  return {
    name: 'mysql',
    status: 'active',
    site_count: 0,
    preset_owned: true,
    is_database: true
  } as Service;
}

describe('ServiceDetail databases tab', () => {
  beforeEach(() => session.update((s) => ({ ...s, role: 'admin' })));

  it('shows the Databases tab to an admin', () => {
    const { getByRole } = render(ServiceDetail, { props: { svc: dbService() } });
    expect(getByRole('button', { name: 'Databases' })).toBeInTheDocument();
  });

  it('hides the Databases tab from a developer', () => {
    session.update((s) => ({ ...s, role: 'developer' }));
    const { queryByRole } = render(ServiceDetail, { props: { svc: dbService() } });
    expect(queryByRole('button', { name: 'Databases' })).toBeNull();
  });
});

describe('ServiceDetail entities tab', () => {
  beforeEach(() => session.update((s) => ({ ...s, role: 'admin' })));

  function entityService(): Service {
    return {
      name: 'rustfs',
      status: 'active',
      site_count: 0,
      preset_owned: true,
      entity_kinds: ['buckets']
    } as Service;
  }

  // What the service holds is the primary thing to look at, so an
  // entity-declaring service lands on its entity tab, not on logs.
  it('opens on the entity tab, named after the declared kind', () => {
    const { getByRole } = render(ServiceDetail, { props: { svc: entityService() } });
    const tab = getByRole('button', { name: 'Buckets' });
    expect(tab.className).toContain('border-servlo-red');
  });

  it('hides the entity tab from a developer', () => {
    session.update((s) => ({ ...s, role: 'developer' }));
    const { queryByRole } = render(ServiceDetail, { props: { svc: entityService() } });
    expect(queryByRole('button', { name: 'Buckets' })).toBeNull();
  });
});
