import { render, screen, fireEvent } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './WorktreePicker.test.svelte';
import type { Site } from '$stores/sites';

function siteWith(worktrees: Site['worktrees'], extra: Partial<Site> = {}): Site {
  return {
    domain: 'acme.test',
    branch: 'main',
    worktrees,
    ...extra
  };
}

describe('WorktreePicker', () => {
  it('shows a passive git:(branch) label when there are no worktrees', () => {
    const { container } = render(Harness, {
      props: { site: siteWith([]), activeBranch: '', onchange: () => {} }
    });
    expect(container.querySelector('button')).toBeNull();
    expect(screen.getByText('git:(main)')).toBeInTheDocument();
  });

  it('renders nothing when worktrees is undefined and no branch is set', () => {
    const { container } = render(Harness, {
      props: {
        site: siteWith(undefined, { branch: '' }),
        activeBranch: '',
        onchange: () => {}
      }
    });
    expect(container.querySelector('button')).toBeNull();
    expect(container.textContent ?? '').not.toContain('git:');
  });

  it('shows the main branch in the pill by default when worktrees exist', () => {
    render(Harness, {
      props: {
        site: siteWith([{ branch: 'feat-a', domain: 'feat-a.acme.test' }]),
        activeBranch: '',
        onchange: () => {}
      }
    });
    expect(screen.getByText('git:(main)')).toBeInTheDocument();
  });

  it('shows the active worktree branch when one is selected', () => {
    render(Harness, {
      props: {
        site: siteWith([{ branch: 'feat-a', domain: 'feat-a.acme.test' }]),
        activeBranch: 'feat-a',
        onchange: () => {}
      }
    });
    expect(screen.getByText('git:(feat-a)')).toBeInTheDocument();
  });

  it('opens the dropdown listing main + worktrees on pill click', async () => {
    render(Harness, {
      props: {
        site: siteWith([
          { branch: 'feat-a', domain: 'feat-a.acme.test' },
          { branch: 'feat-b', domain: 'feat-b.acme.test' }
        ]),
        activeBranch: '',
        onchange: () => {}
      }
    });
    const trigger = screen.getAllByRole('button')[0];
    await fireEvent.click(trigger);
    expect(screen.getAllByText('feat-a').length).toBeGreaterThan(0);
    expect(screen.getAllByText('feat-b').length).toBeGreaterThan(0);
    expect(screen.getAllByText('main').length).toBeGreaterThan(0);
  });

  it('emits onchange with empty string when picking main', async () => {
    const onchange = vi.fn();
    render(Harness, {
      props: {
        site: siteWith([{ branch: 'feat-a', domain: 'feat-a.acme.test' }]),
        activeBranch: 'feat-a',
        onchange
      }
    });
    const trigger = screen.getAllByRole('button')[0];
    await fireEvent.click(trigger);
    // The dropdown row labels the main entry with the literal "main" badge.
    const mainBadges = screen.getAllByText('main');
    const dropdownMain = mainBadges.find((el) => el.tagName === 'SPAN' && el.className.includes('uppercase'));
    expect(dropdownMain).toBeDefined();
    const mainButton = dropdownMain!.closest('button');
    expect(mainButton).not.toBeNull();
    await fireEvent.click(mainButton!);
    expect(onchange).toHaveBeenCalledWith('');
  });

  it('emits onchange with branch name when picking a worktree', async () => {
    const onchange = vi.fn();
    render(Harness, {
      props: {
        site: siteWith([{ branch: 'feat-a', domain: 'feat-a.acme.test' }]),
        activeBranch: '',
        onchange
      }
    });
    const trigger = screen.getAllByRole('button')[0];
    await fireEvent.click(trigger);
    const featButton = screen.getAllByText('feat-a').slice(-1)[0].closest('button');
    expect(featButton).not.toBeNull();
    await fireEvent.click(featButton!);
    expect(onchange).toHaveBeenCalledWith('feat-a');
  });

  it('shows the count of branches (main + worktrees) on the pill', () => {
    render(Harness, {
      props: {
        site: siteWith([
          { branch: 'feat-a', domain: 'feat-a.acme.test' },
          { branch: 'feat-b', domain: 'feat-b.acme.test' }
        ]),
        activeBranch: '',
        onchange: () => {}
      }
    });
    expect(screen.getByText('3')).toBeInTheDocument();
  });
});
