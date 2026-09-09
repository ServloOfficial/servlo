import { describe, it, expect } from 'vitest';
import en from '../../messages/en.json';

// A count of one has to read like one.
//
// "1 sites served" is the shape of defect this project already shipped once, on
// a card nobody had looked at, and it is invisible to every other check: the
// string is correct English until the number in front of it is 1. The house
// pattern is a pair, a singular key beside the plural one, chosen at the call
// site.
//
// So: a message whose count placeholder is followed by a plural noun needs a
// singular sibling. Named exceptions are for a message a count of one cannot
// reach, and each one says why.
const ONE_CANNOT_REACH: Record<string, string> = {
  // Only rendered when the listing was cut short, which is at a few hundred.
  files_truncated: 'only shown when a long listing is truncated',
  // The grouped worker notification is built only for two or more failures; a
  // single failure takes notify_worker_failed_title instead.
  notify_worker_failed_group_title: 'the group push is only built for two or more',
  // No caller. Left in the catalogues rather than deleted here.
  sites_reqstats_samples: 'no call site',
  queries_rollup: 'no call site',
};

const countedPlural = /\{(?:count|n|total|num)\}[^\n]{0,12}?\b([A-Za-z]+s)\b/;

describe('counted messages', () => {
  it('have a singular for a count of one', () => {
    const messages = en as unknown as Record<string, string>;
    const missing: string[] = [];
    for (const [key, value] of Object.entries(messages)) {
      if (typeof value !== 'string' || key.startsWith('$')) continue;
      if (key in ONE_CANNOT_REACH) continue;
      if (key.endsWith('One') || key.endsWith('Single')) continue;
      // "path(s)" is the other way of being right about one, and needs no pair.
      if (value.includes('(s)')) continue;
      if (!countedPlural.test(value)) continue;
      const singular = key.endsWith('Many') ? key.slice(0, -4) + 'One' : key + 'One';
      const alternate = key.replace(/s(On|Served|Count)?$/, '$1');
      if (!(singular in messages) && !(alternate in messages && alternate !== key)) {
        missing.push(`${key}: ${value}`);
      }
    }
    expect(missing).toEqual([]);
  });
});
