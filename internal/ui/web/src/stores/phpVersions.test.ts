import { describe, it, expect } from 'vitest';
import { phpOptionsForSite } from './phpVersions';

describe('phpOptionsForSite', () => {
  const installed = ['7.4', '8.1', '8.3', '8.4', '8.5'];
  const franken = ['8.2', '8.3', '8.4', '8.5'];
  const values = (opts: { value: string }[]) => opts.map((o) => o.value);
  const disabled = (opts: { value: string; disabled?: boolean }[]) =>
    opts.filter((o) => o.disabled).map((o) => o.value);

  it('returns every installed version for non-FrankenPHP runtimes', () => {
    expect(values(phpOptionsForSite('fpm', installed, franken, '8.4'))).toEqual(installed);
    expect(values(phpOptionsForSite(undefined, installed, franken, '8.4'))).toEqual(installed);
  });

  it('limits FrankenPHP to installed versions that are also publishable', () => {
    expect(values(phpOptionsForSite('frankenphp', installed, franken, '8.4'))).toEqual(['8.3', '8.4', '8.5']);
  });

  it('keeps the current version even when it is not FPM-installed', () => {
    expect(values(phpOptionsForSite('frankenphp', ['8.3', '8.4'], franken, '8.5'))).toEqual(['8.3', '8.4', '8.5']);
  });

  it('never offers a version FrankenPHP cannot run', () => {
    expect(values(phpOptionsForSite('frankenphp', installed, franken, '8.4'))).not.toContain('7.4');
    expect(values(phpOptionsForSite('frankenphp', installed, franken, '8.4'))).not.toContain('8.1');
  });

  it('disables nothing when the framework declares no range', () => {
    expect(disabled(phpOptionsForSite('fpm', installed, franken, '8.4'))).toEqual([]);
  });

  it('disables versions outside the framework PHP range', () => {
    // Laravel 10: 8.1–8.3 with the site already on 8.3. Everything below 8.1
    // and above 8.3 is kept in the list but disabled.
    const opts = phpOptionsForSite('fpm', installed, franken, '8.3', '8.1', '8.3');
    expect(values(opts)).toEqual(installed); // all kept, none hidden
    expect(disabled(opts)).toEqual(['7.4', '8.4', '8.5']);
  });

  it('never disables the current version even if it is out of range', () => {
    // A legacy site already pinned to 7.4 keeps 7.4 selectable.
    const opts = phpOptionsForSite('fpm', installed, franken, '7.4', '8.1', '8.3');
    expect(disabled(opts)).toEqual(['8.4', '8.5']);
    expect(disabled(opts)).not.toContain('7.4');
  });
});
