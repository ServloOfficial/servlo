import { describe, it, expect, afterEach, vi } from 'vitest';
import {
  breadcrumbs,
  deleteFileEntry,
  isArchive,
  loadFileContent,
  loadFiles,
  parentOf,
  saveFileContent,
  unzipFile
} from './files';

function mockFetch(body: unknown = { ok: true }) {
  const fn = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => body,
    text: async () => JSON.stringify(body)
  } as Response);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function lastUrl(fn: ReturnType<typeof mockFetch>): string {
  return (fn.mock.calls.at(-1) as [string, RequestInit])[0];
}

describe('file manager API helpers', () => {
  afterEach(() => vi.unstubAllGlobals());

  // A path is opaque to the panel: it goes out encoded and comes back decided.
  // Encoding it is not a security check, it is what stops a name with a slash
  // or a hash in it addressing a different route entirely.
  it('encodes the domain and the path into every request', async () => {
    const fetchMock = mockFetch({ entries: [] });
    await loadFiles('acme.com', 'wp-content/plugins/a b#c');
    expect(lastUrl(fetchMock)).toBe(
      '/api/sites/acme.com/files?path=wp-content%2Fplugins%2Fa%20b%23c'
    );

    await loadFileContent('acme.com', '.env');
    expect(lastUrl(fetchMock)).toBe('/api/sites/acme.com/files/content?path=.env');

    await deleteFileEntry('acme.com', 'old dir/x.php');
    expect(lastUrl(fetchMock)).toBe('/api/sites/acme.com/files/entry?path=old%20dir%2Fx.php');
  });

  it('sends a save as a PUT carrying the path and the content', async () => {
    const fetchMock = mockFetch();
    await saveFileContent('acme.com', 'public/index.php', '<?php');
    const [url, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
    expect(url).toBe('/api/sites/acme.com/files/content');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(String(init.body))).toEqual({
      path: 'public/index.php',
      content: '<?php'
    });
  });

  it('sends an extraction as a POST naming the archive', async () => {
    const fetchMock = mockFetch({ ok: true, files: 3 });
    await unzipFile('acme.com', 'plugins/acme.zip');
    const [url, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
    expect(url).toBe('/api/sites/acme.com/files/unzip');
    expect(JSON.parse(String(init.body))).toEqual({ path: 'plugins/acme.zip' });
  });
});

describe('path helpers', () => {
  it('walks up one level, stopping at the site root', () => {
    expect(parentOf('app/Models/User.php')).toBe('app/Models');
    expect(parentOf('app')).toBe('');
    expect(parentOf('')).toBe('');
  });

  it('builds one breadcrumb per segment, each addressing its own directory', () => {
    expect(breadcrumbs('app/Models')).toEqual([
      { name: 'app', path: 'app' },
      { name: 'Models', path: 'app/Models' }
    ]);
    expect(breadcrumbs('')).toEqual([]);
  });

  it('offers Extract only for a zip', () => {
    expect(isArchive('theme.zip')).toBe(true);
    expect(isArchive('THEME.ZIP')).toBe(true);
    expect(isArchive('backup.tar.gz')).toBe(false);
    expect(isArchive('zip')).toBe(false);
  });
});

describe('editor language', () => {
  it('reads the language off the name, and falls back to plain text', async () => {
    const { languageForFile } = await import('./files');
    expect(languageForFile('public/index.php')).toBe('php');
    expect(languageForFile('composer.json')).toBe('json');
    expect(languageForFile('composer.lock')).toBe('json');
    // A dotenv file is its whole name, not a suffix.
    expect(languageForFile('.env')).toBe('dotenv');
    expect(languageForFile('.env.production')).toBe('dotenv');
    expect(languageForFile('storage/logs/laravel.log')).toBe('plaintext');
    expect(languageForFile('LICENSE')).toBe('plaintext');
  });
});
