import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('addsite', () => {
  const realFetch = globalThis.fetch;
  beforeEach(() => vi.resetModules());
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('asks about a path without changing anything', async () => {
    const seen: Array<{ url: string; method?: string }> = [];
    globalThis.fetch = vi.fn(async (url: string, init?: RequestInit) => {
      seen.push({ url: String(url), method: init?.method });
      return new Response(
        JSON.stringify({ path: '/srv/example.com', exists: true, framework: 'laravel', public_dir: 'public' }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;

    const { inspectDirectory } = await import('./addsite');
    const report = await inspectDirectory('/srv/example.com');

    expect(report.framework).toBe('laravel');
    expect(report.public_dir).toBe('public');
    // A GET, so a form that inspects on every keystroke cannot register a site
    // by accident.
    expect(seen[0].method).toBeUndefined();
    expect(seen[0].url).toContain('/api/sites/inspect');
  });

  it('encodes a path so a space or a hash does not truncate it', async () => {
    const seen: string[] = [];
    globalThis.fetch = vi.fn(async (url: string) => {
      seen.push(String(url));
      return new Response(JSON.stringify({ path: '', exists: false }), { status: 200 });
    }) as unknown as typeof fetch;

    const { inspectDirectory } = await import('./addsite');
    await inspectDirectory('/srv/my site#1');

    expect(seen[0]).toContain('my%20site%231');
  });

  it('surfaces the refusal rather than throwing on it', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response(JSON.stringify({ error: 'myapp is not a fully qualified domain' }), { status: 200 })
    ) as unknown as typeof fetch;

    const { addSite } = await import('./addsite');
    const res = await addSite({ domain: 'myapp', path: '/srv/myapp' });

    expect(res.ok).toBeFalsy();
    expect(res.error).toContain('fully qualified');
  });

  it('posts the form as JSON and returns what was registered', async () => {
    let body = '';
    globalThis.fetch = vi.fn(async (_url: string, init?: RequestInit) => {
      body = String(init?.body);
      return new Response(
        JSON.stringify({ ok: true, domain: 'example.com', name: 'example', php_version: '8.4' }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;

    const { addSite } = await import('./addsite');
    const res = await addSite({
      domain: 'example.com',
      path: '/srv/example.com',
      php_version: '8.3',
      public_dir: 'public'
    });

    expect(JSON.parse(body)).toEqual({
      domain: 'example.com',
      path: '/srv/example.com',
      php_version: '8.3',
      public_dir: 'public'
    });
    expect(res.ok).toBe(true);
    // The response is what got registered, not what was asked for: the linker
    // may have clamped the version to the framework's range.
    expect(res.php_version).toBe('8.4');
  });
});
