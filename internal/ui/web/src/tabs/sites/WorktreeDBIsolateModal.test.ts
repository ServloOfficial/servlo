import { render, screen, fireEvent } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './WorktreeDBIsolateModal.test.svelte';
import type { Site } from '$stores/sites';

function siteWith(worktrees: Site['worktrees']): Site {
  return { domain: 'acme.test', branch: 'main', worktrees };
}

describe('WorktreeDBIsolateModal', () => {
  it('does not render anything when closed', () => {
    const { container } = render(Harness, {
      props: {
        open: false,
        site: siteWith([{ branch: 'feat-a' }]),
        branch: 'feat-a',
        onclose: () => {},
        onconfirm: () => {}
      }
    });
    expect(container.querySelector('select')).toBeNull();
  });

  it('lists main and any other isolated worktrees, but not the active one', async () => {
    render(Harness, {
      props: {
        open: true,
        site: siteWith([
          { branch: 'feat-a', db_isolated: true, db_database: 'acme_feat_a' },
          { branch: 'feat-b', db_isolated: true, db_database: 'acme_feat_b' },
          { branch: 'feat-c', db_isolated: false }
        ]),
        branch: 'feat-a',
        onclose: () => {},
        onconfirm: () => {}
      }
    });
    // Open the popover and read the labels from the listbox.
    const triggers = screen.getAllByRole('button');
    const trigger = triggers.find((b) => b.getAttribute('aria-haspopup') === 'listbox')!;
    trigger.click();
    await new Promise((r) => setTimeout(r, 0));
    const items = Array.from(document.querySelectorAll('[role="option"]')).map(
      (n) => n.textContent?.toLowerCase() ?? ''
    );
    expect(items.some((t) => t.includes('main'))).toBe(true);
    expect(items.some((t) => t.includes('feat-b'))).toBe(true);
    expect(items.some((t) => t.includes('feat-a'))).toBe(false);
    expect(items.some((t) => t.includes('feat-c'))).toBe(false);
  });

  it('emits onconfirm with the selected value and closes the modal', async () => {
    const onconfirm = vi.fn();
    const onclose = vi.fn();
    render(Harness, {
      props: {
        open: true,
        site: siteWith([{ branch: 'feat-a' }]),
        branch: 'feat-a',
        onclose,
        onconfirm
      }
    });
    // Open the popover and click the "empty" option, then Isolate.
    const trigger = screen
      .getAllByRole('button')
      .find((b) => b.getAttribute('aria-haspopup') === 'listbox')!;
    trigger.click();
    await new Promise((r) => setTimeout(r, 0));
    const options = Array.from(document.querySelectorAll('[role="option"]')) as HTMLButtonElement[];
    // The "Start empty" option matches the m.worktreeDb_empty() label.
    const emptyOpt = options[1] ?? options[0];
    emptyOpt.click();
    await new Promise((r) => setTimeout(r, 0));
    const isolateBtn = screen.getByText('Isolate');
    await fireEvent.click(isolateBtn);
    expect(onconfirm).toHaveBeenCalledWith('');
    expect(onclose).toHaveBeenCalled();
  });

  it('emits onclose when Cancel is clicked', async () => {
    const onclose = vi.fn();
    const onconfirm = vi.fn();
    render(Harness, {
      props: {
        open: true,
        site: siteWith([{ branch: 'feat-a' }]),
        branch: 'feat-a',
        onclose,
        onconfirm
      }
    });
    await fireEvent.click(screen.getByText('Cancel'));
    expect(onclose).toHaveBeenCalled();
    expect(onconfirm).not.toHaveBeenCalled();
  });

  it('defaults the selection to "main"', () => {
    render(Harness, {
      props: {
        open: true,
        site: siteWith([{ branch: 'feat-a' }]),
        branch: 'feat-a',
        onclose: () => {},
        onconfirm: () => {}
      }
    });
    // The trigger label shows the selected option's label — for value "main"
    // that's the m.worktreeDb_cloneFromMain key. Just verify the trigger
    // text isn't the empty / placeholder fallback.
    const trigger = screen
      .getAllByRole('button')
      .find((b) => b.getAttribute('aria-haspopup') === 'listbox')!;
    expect(trigger.textContent?.toLowerCase() ?? '').toContain('main');
  });
});
