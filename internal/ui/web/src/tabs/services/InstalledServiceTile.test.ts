import { render } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import InstalledServiceTile from './InstalledServiceTile.svelte';
import type { Service } from '$stores/services';

function svc(over: Partial<Service> = {}): Service {
  return { name: 'mysql', status: 'inactive', site_count: 0, ...over } as Service;
}

describe('InstalledServiceTile', () => {
  it('renders the human label for the service', () => {
    const { getByText } = render(InstalledServiceTile, { props: { svc: svc() } });
    expect(getByText('MySQL')).toBeTruthy();
  });

  it('shows a green dot when active', () => {
    const { container } = render(InstalledServiceTile, { props: { svc: svc({ status: 'active' }) } });
    expect(container.querySelector('.bg-emerald-500')).toBeTruthy();
  });

  it('shows version and site count when present', () => {
    const { getByText } = render(InstalledServiceTile, {
      props: { svc: svc({ version: 'v8.4', site_count: 3 }) }
    });
    expect(getByText('v8.4')).toBeTruthy();
    expect(getByText('3')).toBeTruthy();
  });

  it('shows the update arrow when an update is available', () => {
    const { getByText } = render(InstalledServiceTile, {
      props: { svc: svc({ update_available: true }) }
    });
    expect(getByText('↑')).toBeTruthy();
  });
});
