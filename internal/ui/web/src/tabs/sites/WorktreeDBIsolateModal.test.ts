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

  it('lists main and any other isolated worktrees, but not the active one', () => {
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
    const select = screen.getByRole('combobox') as HTMLSelectElement;
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).toContain('main');
    expect(values).toContain('');
    expect(values).toContain('feat-b');
    expect(values).not.toContain('feat-a');
    expect(values).not.toContain('feat-c');
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
    const select = screen.getByRole('combobox') as HTMLSelectElement;
    select.value = '';
    await fireEvent.change(select);
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
    const select = screen.getByRole('combobox') as HTMLSelectElement;
    expect(select.value).toBe('main');
  });
});
