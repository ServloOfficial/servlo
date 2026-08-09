import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('dashboard store', () => {
  beforeEach(() => {
    location.hash = '';
  });

  it('proxied bundled dashboard embeds in the overlay and lists in the sidebar', async () => {
    const { services } = await import('./services');
    const { openDashboard, dashboardOpen, dashboardServices } = await import('./dashboard');

    const rabbit = {
      name: 'rabbitmq',
      status: 'active',
      site_count: 0,
      dashboard: '/_svc/rabbitmq/',
      dashboard_external: false
    };
    services.set([rabbit]);

    expect(get(dashboardServices).some((s) => s.name === 'rabbitmq')).toBe(true);

    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    openDashboard(rabbit);
    expect(open).not.toHaveBeenCalled();
    expect(get(dashboardOpen)?.dashboard).toBe('/_svc/rabbitmq/');
    expect(location.hash).toBe('#service/rabbitmq');
    open.mockRestore();
  });

  it('user external dashboard still opens in a new tab and is not embedded', async () => {
    const { services } = await import('./services');
    const { openDashboard, dashboardOpen, dashboardServices } = await import('./dashboard');

    const ext = {
      name: 'myadmin',
      status: 'active',
      site_count: 0,
      dashboard: 'http://localhost:9000',
      dashboard_external: true
    };
    services.set([ext]);
    dashboardOpen.set(null);

    expect(get(dashboardServices).some((s) => s.name === 'myadmin')).toBe(false);

    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    openDashboard(ext);
    expect(open).toHaveBeenCalledWith('http://localhost:9000', '_blank', 'noopener,noreferrer');
    expect(get(dashboardOpen)).toBeNull();
    open.mockRestore();
  });

  it('openDocs embeds the docs landing page in the overlay', async () => {
    const { openDocs, dashboardOpen } = await import('./dashboard');

    dashboardOpen.set(null);
    openDocs();
    const cur = get(dashboardOpen);
    expect(cur?.name).toBe('docs');
    expect(cur?.dashboard).toBe('https://realrashid.github.io/servlo/getting-started/requirements');
    expect(location.hash).toBe('#docs');
  });
});
