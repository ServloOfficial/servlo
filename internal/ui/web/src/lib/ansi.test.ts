import { describe, it, expect } from 'vitest';
import { ansiToHtml } from './ansi';

describe('ansiToHtml', () => {
  it('passes plain text through with HTML escaping', () => {
    expect(ansiToHtml('hello world')).toBe('hello world');
    expect(ansiToHtml('<script>')).toBe('&lt;script&gt;');
    expect(ansiToHtml('a & b > c')).toBe('a &amp; b &gt; c');
  });

  it('strips reset and renders nothing for default colors', () => {
    expect(ansiToHtml('\x1b[0mtext')).toBe('text');
  });

  it('wraps red foreground in a span', () => {
    const html = ansiToHtml('\x1b[31mboom\x1b[0m');
    expect(html).toContain('color:');
    expect(html).toContain('boom');
    expect(html).toContain('</span>');
  });

  it('closes the open span at end of input even without explicit reset', () => {
    const html = ansiToHtml('\x1b[32mgreen-no-reset');
    expect(html).toContain('green-no-reset');
    expect(html.match(/<\/span>/g)).toHaveLength(1);
  });

  it('handles bold + color together', () => {
    const html = ansiToHtml('\x1b[1;31mERROR\x1b[0m: details');
    expect(html).toContain('font-weight:600');
    expect(html).toContain('color:');
    expect(html).toContain('ERROR');
    expect(html).toContain('details');
  });

  it('strips cursor-positioning escapes without rendering them', () => {
    // \x1b[2K clears the line, \x1b[1A moves cursor up — both should vanish.
    const html = ansiToHtml('\x1b[2K\x1b[1Akept');
    expect(html).toBe('kept');
  });

  it('handles 256-color foreground by mapping to nearest 8-color', () => {
    const html = ansiToHtml('\x1b[38;5;9mbright-red\x1b[0m');
    expect(html).toContain('bright-red');
    expect(html).toContain('color:');
  });

  it('preserves newlines', () => {
    expect(ansiToHtml('one\ntwo')).toBe('one\ntwo');
  });

  it('handles back-to-back color changes without nesting spans', () => {
    const html = ansiToHtml('\x1b[31mred\x1b[32mgreen\x1b[0m');
    // We should never have a <span> inside a <span> — closing the previous
    // span happens before opening the next.
    const opens = html.match(/<span/g)?.length ?? 0;
    const closes = html.match(/<\/span>/g)?.length ?? 0;
    expect(opens).toBe(closes);
  });
});
