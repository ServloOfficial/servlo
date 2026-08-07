import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { status } from '$stores/status';
import ProductionBadge from './ProductionBadge.svelte';

describe('ProductionBadge', () => {
  beforeEach(() => status.set(null as never));

  // Both states are shown. A badge that only appears in production leaves the
  // other mode indistinguishable from "the badge is broken".
  it('says development when production mode is off', () => {
    status.set({ production: false } as never);
    render(ProductionBadge);
    expect(screen.getByText('Development')).toBeInTheDocument();
  });

  it('says production when it is on', () => {
    status.set({ production: true } as never);
    render(ProductionBadge);
    expect(screen.getByText('Production')).toBeInTheDocument();
  });

  // An absent field means an older panel or a failed status fetch. Claiming
  // production there would tell someone their errors are hidden when they are
  // not, which is the more dangerous of the two wrong answers.
  it('does not claim production when the status has not loaded', () => {
    status.set({} as never);
    render(ProductionBadge);
    expect(screen.getByText('Development')).toBeInTheDocument();
  });

  it('marks the state on the element so the shell can be asserted on', () => {
    status.set({ production: true } as never);
    const { container } = render(ProductionBadge);
    expect(container.querySelector('[data-production="true"]')).not.toBeNull();
  });
});
