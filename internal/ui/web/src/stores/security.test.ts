import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { security, securityLoaded, loadSecurity, addKey, removeKey } from './security';

function respond(body: unknown, ok = true) {
  return Promise.resolve({ ok, json: () => Promise.resolve(body) } as Response);
}

const alex = { type: 'ssh-ed25519', comment: 'alex', fingerprint: 'SHA256:aaa' };
const sam = { type: 'ssh-rsa', comment: 'sam', fingerprint: 'SHA256:bbb' };

const answer = {
  findings: [{ severity: 'bad', title: 'ports open', fix: ['sudo ufw --force enable'] }],
  firewall: { why: 'because', commands: ['sudo ufw --force enable'], open: [22], unexpected: [3306] },
  fail2ban: { running: true, status_command: 'sudo fail2ban-client status sshd', why: 'root' },
  provider: { name: 'DigitalOcean', detail: 'a droplet' },
  keys: [alex, sam],
  keys_path: '/home/deploy/.ssh/authorized_keys',
  ssh_port: 22
};

describe('the security store', () => {
  beforeEach(() => {
    securityLoaded.set(false);
    vi.restoreAllMocks();
  });

  it('loads the audit, the plan and the keys', async () => {
    vi.stubGlobal('fetch', vi.fn(() => respond(answer)));
    await loadSecurity();

    const s = get(security);
    expect(s.findings).toHaveLength(1);
    expect(s.firewall.commands[0]).toContain('ufw');
    expect(s.keys.map((k) => k.comment)).toEqual(['alex', 'sam']);
    expect(get(securityLoaded)).toBe(true);
  });

  // An audit that blanks itself because one request failed reads as a server
  // with nothing wrong, which is the opposite of what it is for.
  it('leaves the last answer standing when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => respond(answer)));
    await loadSecurity();
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('offline'))));
    await loadSecurity();

    expect(get(security).findings).toHaveLength(1);
  });

  it('replaces the key list with what the server says is left', async () => {
    vi.stubGlobal('fetch', vi.fn(() => respond(answer)));
    await loadSecurity();

    vi.stubGlobal('fetch', vi.fn(() => respond({ ok: true, keys: [sam] })));
    expect(await removeKey('SHA256:aaa')).toBeNull();
    expect(get(security).keys.map((k) => k.comment)).toEqual(['sam']);
  });

  // A refused add keeps the list on screen and hands back the reason, because
  // the reason is the whole value: somebody pasting a private key does not know
  // they have done it.
  it('reports why an add was refused without emptying the list', async () => {
    vi.stubGlobal('fetch', vi.fn(() => respond(answer)));
    await loadSecurity();

    vi.stubGlobal('fetch', vi.fn(() => respond({ ok: false, keys: [alex, sam], error: 'that is a private key' })));
    expect(await addKey('-----BEGIN', 'alex')).toBe('that is a private key');
    expect(get(security).keys).toHaveLength(2);
  });
});
