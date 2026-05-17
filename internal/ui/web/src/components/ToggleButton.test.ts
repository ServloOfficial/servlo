import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './ToggleButton.test.svelte';

describe('ToggleButton', () => {
  it('renders the label', () => {
    render(Harness, { props: { label: 'HTTPS' } });
    expect(screen.getByRole('button', { name: 'HTTPS' })).toBeInTheDocument();
  });

  it('shows a green dot when on', () => {
    const { container } = render(Harness, { props: { label: 'queue', on: true } });
    const dot = container.querySelector('span.rounded-full');
    expect(dot?.className).toMatch(/bg-emerald-500/);
  });

  it('shows a red dot when failing (overrides on)', () => {
    const { container } = render(Harness, { props: { label: 'queue', on: true, failing: true } });
    const dot = container.querySelector('span.rounded-full');
    expect(dot?.className).toMatch(/bg-red-500/);
  });

  it('shows an outlined gray dot when off', () => {
    const { container } = render(Harness, { props: { label: 'LAN', on: false } });
    const dot = container.querySelector('span.rounded-full');
    expect(dot?.className).toMatch(/border-gray-300|border-gray-600/);
  });

  it('shows an amber spinner when loading', () => {
    const { container } = render(Harness, { props: { label: 'queue', loading: true } });
    expect(container.querySelector('svg.animate-spin')).toBeInTheDocument();
    expect(container.querySelector('span.rounded-full')).not.toBeInTheDocument();
  });

  it('tints background emerald when on', () => {
    render(Harness, { props: { label: 'queue', on: true } });
    const btn = screen.getByRole('button', { name: 'queue' });
    expect(btn.className).toMatch(/emerald/);
  });

  it('tints background red when failing', () => {
    render(Harness, { props: { label: 'queue', on: true, failing: true } });
    const btn = screen.getByRole('button', { name: 'queue' });
    expect(btn.className).toMatch(/red/);
  });

  it('forwards click', () => {
    const onclick = vi.fn();
    render(Harness, { props: { label: 'queue', onclick } });
    screen.getByRole('button', { name: 'queue' }).click();
    expect(onclick).toHaveBeenCalledOnce();
  });

  it('disables button when disabled is true', () => {
    render(Harness, { props: { label: 'queue', disabled: true } });
    const btn = screen.getByRole('button', { name: 'queue' }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });
});
