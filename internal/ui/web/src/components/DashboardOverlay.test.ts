import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, beforeEach } from 'vitest';
import DashboardOverlay from './DashboardOverlay.svelte';
import { dashboardOpen } from '../stores/dashboard';

function openService() {
  dashboardOpen.set({
    name: 'rustfs',
    label: 'RustFS',
    dashboard: 'http://localhost:9001'
  });
}

describe('DashboardOverlay', () => {
  beforeEach(() => {
    dashboardOpen.set(null);
  });

  it('renders nothing until a dashboard is opened', () => {
    const { container } = render(DashboardOverlay);
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('embeds the opened dashboard and labels the frame with it', () => {
    openService();
    const { container } = render(DashboardOverlay);

    const frame = container.querySelector('iframe');
    expect(frame?.getAttribute('src')).toBe('http://localhost:9001');
    expect(frame?.getAttribute('title')).toBe('RustFS');
  });

  it('offers the dashboard in a new tab as an escape from the iframe', () => {
    openService();
    render(DashboardOverlay);

    const link = screen.getByTitle('Open in new tab');
    expect(link.getAttribute('href')).toBe('http://localhost:9001');
    expect(link.getAttribute('rel')).toBe('noopener');
  });
});
