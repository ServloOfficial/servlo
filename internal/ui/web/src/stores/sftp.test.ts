import { describe, it, expect, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { authoriseSFTPKey, loadSFTP, sftpStatus, withdrawSFTPKey } from './sftp';

function mockFetch(body: unknown) {
  const fn = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => body,
    text: async () => JSON.stringify(body)
  } as Response);
  vi.stubGlobal('fetch', fn);
  return fn;
}

describe('SFTP status', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('loads the sites, their ports and whether sshd confines them', async () => {
    mockFetch({
      user: 'deploy',
      installed: true,
      drifted: false,
      staged_path: '/home/deploy/.config/servlo/sftp/60-servlo-sftp.conf',
      commands: ['sudo sshd -t'],
      sites: [{ domain: 'acme.com', path: '/srv/acme', port: 2200, confined: true, keys: [] }]
    });

    await loadSFTP();

    const status = get(sftpStatus);
    expect(status.user).toBe('deploy');
    expect(status.sites[0].port).toBe(2200);
    expect(status.sites[0].confined).toBe(true);
  });

  // A status the panel cannot read must not read as "confined": the banner that
  // warns an operator their keys are unconfined is the one that has to survive
  // a failure.
  it('reports nothing confined when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));

    await loadSFTP();

    const status = get(sftpStatus);
    expect(status.installed).toBe(false);
    expect(status.sites).toEqual([]);
    expect(status.error).toBeTruthy();
  });

  it('authorises a key with its site and label', async () => {
    const fetchMock = mockFetch({ ok: true });

    await authoriseSFTPKey('acme.com', 'alice-laptop', 'ssh-ed25519 AAAA');

    const [url, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
    expect(url).toBe('/api/sftp');
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toEqual({
      domain: 'acme.com',
      label: 'alice-laptop',
      key: 'ssh-ed25519 AAAA'
    });
  });

  it('withdraws by fingerprint, encoded, because a fingerprint carries a slash', async () => {
    const fetchMock = mockFetch({ ok: true });

    await withdrawSFTPKey('SHA256:JxZlDUD5LXn0p3ds8TvW2al0AZAmpvu1pWkeuW/xAMk');

    const [url, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
    expect(url).toBe(
      '/api/sftp/keys/SHA256%3AJxZlDUD5LXn0p3ds8TvW2al0AZAmpvu1pWkeuW%2FxAMk'
    );
    expect(init.method).toBe('DELETE');
  });
});
