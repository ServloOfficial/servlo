import { describe, it, expect } from 'vitest';
import { namesTheSource } from './SiteDeployExcludeCard.svelte';

describe('namesTheSource', () => {
  // Every framework but WordPress declares no protected paths, so this is what
  // the card looks like on most sites: it said "Following the framework's list"
  // directly above "Nothing is protected".
  it('says nothing about a framework list that is empty', () => {
    expect(namesTheSource(false, 0)).toBe(false);
  });

  it('names the framework when it does protect something', () => {
    expect(namesTheSource(false, 2)).toBe(true);
  });

  // A site that emptied its own list chose that, and the card should still say
  // the list is the site's rather than leave the warning unattributed.
  it('names the site whether or not its own list is empty', () => {
    expect(namesTheSource(true, 0)).toBe(true);
    expect(namesTheSource(true, 3)).toBe(true);
  });
});
