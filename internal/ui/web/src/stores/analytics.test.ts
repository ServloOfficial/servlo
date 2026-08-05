import { describe, it, expect, vi, beforeEach } from 'vitest';

const apiJson = vi.fn();
vi.mock('$lib/api', () => ({ apiJson: (...args: unknown[]) => apiJson(...args) }));

import { loadSiteAnalytics } from './analytics';

describe('loadSiteAnalytics', () => {
  beforeEach(() => apiJson.mockReset().mockResolvedValue({}));

  it('requests the analytics endpoint with the range', async () => {
    await loadSiteAnalytics('acme.test', '24h');
    expect(apiJson).toHaveBeenCalledWith('/api/sites/acme.test/analytics?range=24h');
  });


  it('encodes a domain with unusual characters', async () => {
    await loadSiteAnalytics('a b.test', '15m');
    expect(apiJson).toHaveBeenCalledWith('/api/sites/a%20b.test/analytics?range=15m');
  });
});
