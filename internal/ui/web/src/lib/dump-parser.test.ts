import { describe, it, expect } from 'vitest';
import { parseDump, looksLikeDump } from './dump-parser';

describe('parseDump', () => {
  it('detects dump prefix', () => {
    expect(looksLikeDump('array:2 [\n  0 => 1\n]')).toBe(true);
    expect(looksLikeDump('App\\Models\\User {#42\n  +id: 1\n}')).toBe(true);
    expect(looksLikeDump('hello world')).toBe(false);
    expect(looksLikeDump('1\n')).toBe(false);
  });

  it('parses an empty array', () => {
    const r = parseDump('[]');
    expect(r.ok).toBe(true);
    expect(r.nodes[0]).toEqual({ kind: 'array', count: 0, items: [] });
  });

  it('parses a flat array', () => {
    const r = parseDump('array:3 [\n  0 => 1\n  1 => "two"\n  2 => null\n]');
    expect(r.ok).toBe(true);
    const a = r.nodes[0];
    expect(a.kind).toBe('array');
    if (a.kind !== 'array') return;
    expect(a.count).toBe(3);
    expect(a.items).toHaveLength(3);
    expect(a.items[0]).toEqual({ key: '0', value: { kind: 'scalar', type: 'number', value: '1' } });
    expect(a.items[1]).toEqual({ key: '1', value: { kind: 'scalar', type: 'string', value: '"two"' } });
    expect(a.items[2]).toEqual({ key: '2', value: { kind: 'scalar', type: 'null', value: 'null' } });
  });

  it('parses an object with mixed visibilities', () => {
    const text = [
      'App\\Models\\User {#42',
      '  +id: 1',
      '  #table: "users"',
      '  -secret: "x"',
      '}'
    ].join('\n');
    const r = parseDump(text);
    expect(r.ok).toBe(true);
    const o = r.nodes[0];
    expect(o.kind).toBe('object');
    if (o.kind !== 'object') return;
    expect(o.class).toBe('App\\Models\\User');
    expect(o.id).toBe(42);
    expect(o.props).toHaveLength(3);
    expect(o.props[0].visibility).toBe('public');
    expect(o.props[1].visibility).toBe('protected');
    expect(o.props[2].visibility).toBe('private');
  });

  it('parses nested object inside array', () => {
    const text = [
      'Illuminate\\Support\\Collection {#1',
      '  #items: array:1 [',
      '    0 => App\\Models\\User {#2',
      '      +id: 1',
      '    }',
      '  ]',
      '}'
    ].join('\n');
    const r = parseDump(text);
    expect(r.ok).toBe(true);
    const root = r.nodes[0];
    expect(root.kind).toBe('object');
    if (root.kind !== 'object') return;
    const items = root.props[0].value;
    expect(items.kind).toBe('array');
    if (items.kind !== 'array') return;
    expect(items.count).toBe(1);
    const inner = items.items[0].value;
    expect(inner.kind).toBe('object');
    if (inner.kind !== 'object') return;
    expect(inner.class).toBe('App\\Models\\User');
    expect(inner.id).toBe(2);
    expect(inner.props[0].name).toBe('id');
  });

  it('handles inline-closed object (reference)', () => {
    const r = parseDump('App\\Models\\User {#42}');
    expect(r.ok).toBe(true);
    const o = r.nodes[0];
    expect(o.kind).toBe('object');
    if (o.kind !== 'object') return;
    expect(o.ref).toBe(true);
    expect(o.props).toHaveLength(0);
  });

  it('returns ok=false on plain text', () => {
    const r = parseDump('hello world');
    expect(r.ok).toBe(false);
  });
});
