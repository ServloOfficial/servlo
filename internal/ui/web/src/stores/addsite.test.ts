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

describe('clone', () => {
  const realFetch = globalThis.fetch;
  beforeEach(() => vi.resetModules());
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('asks for the deploy key by domain and gets back only the public half', async () => {
    let body = '';
    globalThis.fetch = vi.fn(async (_url: string, init?: RequestInit) => {
      body = String(init?.body);
      return new Response(JSON.stringify({ ok: true, public: 'ssh-ed25519 AAAA servlo-example.com' }), {
        status: 200
      });
    }) as unknown as typeof fetch;

    const { deployKeyFor } = await import('./addsite');
    const res = await deployKeyFor('example.com');

    expect(JSON.parse(body)).toEqual({ domain: 'example.com' });
    expect(res.public).toContain('ssh-ed25519 ');
  });

  // "Connection failure gives a specific reason, not a generic error" is the
  // acceptance criterion, so the reason has to survive the round trip intact.
  it('carries the specific failure reason back to the form', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            ok: false,
            reason: 'the host refused the key: add the deploy key above to the repository'
          }),
          { status: 200 }
        )
    ) as unknown as typeof fetch;

    const { testClone } = await import('./addsite');
    const res = await testClone({ domain: 'example.com', repository: 'git@github.com:a/b.git' });

    expect(res.ok).toBe(false);
    expect(res.reason).toContain('add the deploy key');
  });

  it('posts the clone with everything the form collected', async () => {
    let body = '';
    globalThis.fetch = vi.fn(async (_url: string, init?: RequestInit) => {
      body = String(init?.body);
      return new Response(JSON.stringify({ ok: true, domain: 'example.com' }), { status: 200 });
    }) as unknown as typeof fetch;

    const { cloneSite } = await import('./addsite');
    await cloneSite({
      domain: 'example.com',
      path: '/srv/example.com',
      repository: 'git@github.com:a/b.git',
      php_version: '8.4'
    });

    expect(JSON.parse(body).repository).toBe('git@github.com:a/b.git');
    expect(JSON.parse(body).domain).toBe('example.com');
  });
});

describe('upload', () => {
  const realFetch = globalThis.fetch;
  beforeEach(() => vi.resetModules());
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('sends the archive as multipart with the rest of the form', async () => {
    let sent: FormData | null = null;
    let headers: HeadersInit | undefined;
    globalThis.fetch = vi.fn(async (_url: string, init?: RequestInit) => {
      sent = init?.body as FormData;
      headers = init?.headers;
      return new Response(JSON.stringify({ ok: true, domain: 'example.com' }), { status: 200 });
    }) as unknown as typeof fetch;

    const { uploadSite } = await import('./addsite');
    await uploadSite({
      domain: 'example.com',
      path: '/srv/example.com',
      php_version: '8.4',
      archive: new File([new Uint8Array([80, 75, 3, 4])], 'site.zip', { type: 'application/zip' })
    });

    expect(sent).toBeInstanceOf(FormData);
    expect(sent!.get('domain')).toBe('example.com');
    expect(sent!.get('php_version')).toBe('8.4');
    expect((sent!.get('archive') as File).name).toBe('site.zip');
    // Setting Content-Type by hand would omit the boundary and the server would
    // read an empty form.
    const sentHeaders = new Headers(headers);
    expect(sentHeaders.get('Content-Type')).toBeNull();
  });

  it('surfaces a refusal rather than throwing on it', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response(JSON.stringify({ error: 'the archive entry points outside the site directory' }), { status: 200 })
    ) as unknown as typeof fetch;

    const { uploadSite } = await import('./addsite');
    const res = await uploadSite({
      domain: 'example.com',
      path: '/srv/x',
      archive: new File([new Uint8Array([1])], 'bad.zip')
    });

    expect(res.ok).toBeFalsy();
    expect(res.error).toContain('outside the site directory');
  });
});
