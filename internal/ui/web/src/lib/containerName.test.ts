import { describe, it, expect } from 'vitest';
import { shortContainerName } from './containerName';

describe('shortContainerName', () => {
  it('drops the whole prefix', () => {
    expect(shortContainerName('servlo-panel')).toBe('panel');
    expect(shortContainerName('servlo-php84-fpm')).toBe('php84-fpm');
    expect(shortContainerName('servlo-mysql')).toBe('mysql');
  });

  // A container servlo did not create keeps the name it has, and one that only
  // looks like the prefix is not shortened into nonsense.
  it('leaves anything else alone', () => {
    expect(shortContainerName('smtp.postmarkapp.com')).toBe('smtp.postmarkapp.com');
    expect(shortContainerName('servlofoo')).toBe('servlofoo');
    expect(shortContainerName('')).toBe('');
  });
});
