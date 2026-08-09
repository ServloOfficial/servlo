import { render } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import type { CronEntry } from '$stores/cron';

import SiteCronRow from './SiteCronRow.svelte';
import { m } from '../../paraglide/messages.js';

const base: CronEntry = {
  id: 'prune',
  name: 'Prune batches',
  command: 'php artisan queue:prune-batches',
  schedule: '0 3 * * *',
  calendar: '*-*-* 03:00:00',
  capture_output: true,
  disabled: false,
  unit: 'servlo-cron-acme-prune'
};

const props = (entry: Partial<CronEntry>) =>
  ({ entry: { ...base, ...entry }, onedit: () => {}, ondelete: () => {} }) as never;

describe('SiteCronRow', () => {
  // The four states have to be distinguishable at a glance. A row that reads
  // "never run" when it actually failed is the one nobody investigates.
  it('tells never-run, failed, running and stopped apart', () => {
    expect(render(SiteCronRow, { props: props({}) }).getByText(m.sites_cron_neverRun())).toBeTruthy();

    const failed = render(SiteCronRow, {
      props: props({
        last_run: { at: '2026-08-09T03:00:00Z', ok: false, running: false, exit_code: 2, result: 'exit-code' }
      })
    });
    expect(failed.getByText(m.sites_cron_lastRunFailed())).toBeTruthy();
    expect(failed.getByText(m.sites_cron_exit({ result: 'exit-code', code: 2 }))).toBeTruthy();

    const running = render(SiteCronRow, {
      props: props({
        last_run: { at: '2026-08-09T03:00:00Z', ok: false, running: true, exit_code: 0 }
      })
    });
    expect(running.getByText(m.sites_cron_running())).toBeTruthy();

    const stopped = render(SiteCronRow, { props: props({ disabled: true }) });
    expect(stopped.getByText(m.sites_cron_notScheduled())).toBeTruthy();
  });

  // The output is behind a toggle rather than always open: five rows of stack
  // trace each would bury the list.
  it('offers the output only when there is some', () => {
    const withOutput = render(SiteCronRow, {
      props: props({
        last_run: {
          at: '2026-08-09T03:00:00Z',
          ok: true,
          running: false,
          exit_code: 0,
          result: 'success',
          output: ['3 entries deleted.']
        }
      })
    });
    expect(withOutput.getByText(m.sites_cron_showOutput())).toBeTruthy();
    expect(withOutput.queryByText('3 entries deleted.')).toBeNull();

    // An entry that keeps no output says so, rather than leaving a gap that
    // reads as a run which printed nothing.
    const quiet = render(SiteCronRow, {
      props: props({
        capture_output: false,
        last_run: { at: '2026-08-09T03:00:00Z', ok: true, running: false, exit_code: 0, result: 'success' }
      })
    });
    expect(quiet.getByText(m.sites_cron_noCapture())).toBeTruthy();
  });

  // A framework-managed entry has its own switch, and editing it here would be
  // undone the next time the switch was used.
  it('does not let a managed entry be edited or deleted from its row', () => {
    const managed = render(SiteCronRow, { props: props({ managed: true }) });
    expect(managed.getByText(m.sites_cron_managed())).toBeTruthy();
    expect((managed.getByText(m.sites_cron_edit()) as HTMLButtonElement).disabled).toBe(true);
    expect((managed.getByText(m.sites_cron_delete()) as HTMLButtonElement).disabled).toBe(true);
  });

  // The schedule is shown as the operator typed it, not as the systemd
  // expression it became: they have to recognise their own line.
  it('shows the schedule as it was typed', () => {
    const { getByText } = render(SiteCronRow, { props: props({}) });
    expect(getByText('0 3 * * *')).toBeTruthy();
  });
});
