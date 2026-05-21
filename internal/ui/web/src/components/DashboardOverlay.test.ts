import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, beforeEach } from 'vitest';
import DashboardOverlay from './DashboardOverlay.svelte';
import { dashboardOpen } from '../stores/dashboard';

describe('DashboardOverlay', () => {
  beforeEach(() => {
    dashboardOpen.set(null);
  });

  it('disables Back until the embedded iframe has somewhere to go back to', () => {
    dashboardOpen.set({
      name: 'profiler',
      label: 'Profiler',
      dashboard: '/_spx/?SPX_UI_URI=/'
    });
    render(DashboardOverlay);

    // Freshly opened: the SPX iframe has no internal history yet, so Back is a
    // dead end. It must be disabled rather than silently tear down the overlay.
    expect(screen.getByTitle('Back')).toBeDisabled();
  });
});
