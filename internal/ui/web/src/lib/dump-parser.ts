// Parses Symfony VarDumper CLI output (the format produced by Laravel's
// `dump()`) into a tree we can render with collapsible nodes.
//
// The format is line-oriented:
//   Class\Name {#42
//     +public_prop: "value"
//     #protected: array:2 [
//       0 => "a"
//       1 => 1
//     ]
//   }
//
// We don't aim for byte-perfect parsing of every Symfony edge case,
// only "good enough" for typical tinker output. Anything we can't
// parse falls back to rendering the original text.

export type ScalarKind = 'string' | 'number' | 'bool' | 'null' | 'other';
export type Visibility = 'public' | 'protected' | 'private' | 'readonly' | '';

export interface ObjectNode {
  kind: 'object';
  class: string;
  id?: number;
  ref: boolean;
  props: Property[];
}
export interface ArrayNode {
  kind: 'array';
  count: number;
  items: ArrayItem[];
}
export interface ScalarNode {
  kind: 'scalar';
  type: ScalarKind;
  value: string;
}
export type DumpNode = ObjectNode | ArrayNode | ScalarNode;

export interface Property {
  name: string;
  visibility: Visibility;
  value: DumpNode;
}
export interface ArrayItem {
  key: string;
  value: DumpNode;
}

export interface ParseResult {
  ok: boolean;
  nodes: DumpNode[];
  trailing: string;
}

const OBJECT_OPEN_RE = /^(?<class>[A-Za-z_\\][\w\\.]*) \{(?:#(?<id>\d+))?\s*$/;
const OBJECT_INLINE_RE = /^(?<class>[A-Za-z_\\][\w\\.]*) \{(?:#(?<id>\d+))?\s*(?:…|\.\.\.)?\s*\}$/;
const ARRAY_OPEN_RE = /^array:(?<count>\d+) \[\s*$/;
const ARRAY_EMPTY_RE = /^(?:array:0 )?\[\]$/;
const PROP_RE = /^(?<vis>[+#\-~])(?<name>[\w]+):\s*(?<rest>.*)$/;
const ENTRY_RE = /^(?<key>"[^"]*"|\d+)\s*=>\s*(?<rest>.*)$/;

function classifyScalar(raw: string): ScalarKind {
  if (raw === 'null') return 'null';
  if (raw === 'true' || raw === 'false') return 'bool';
  if (/^-?\d+(\.\d+)?$/.test(raw)) return 'number';
  if (raw.startsWith('"') && raw.endsWith('"')) return 'string';
  return 'other';
}

function isScalarLine(s: string): boolean {
  if (s === 'null' || s === 'true' || s === 'false') return true;
  if (/^-?\d+(\.\d+)?$/.test(s)) return true;
  // String literal — accept anything wrapped in matching quotes (Symfony
  // doesn't escape `"` inside dumped strings, it appends `... N more chars`
  // for truncation; we deliberately don't try to validate inner contents).
  if (s.length >= 2 && s.startsWith('"') && s.endsWith('"')) return true;
  return false;
}

function visibilitySymbol(v: string): Visibility {
  switch (v) {
    case '+': return 'public';
    case '#': return 'protected';
    case '-': return 'private';
    case '~': return 'readonly';
    default: return '';
  }
}

class Parser {
  private lines: string[];
  private idx = 0;

  constructor(text: string) {
    this.lines = text.replace(/\r\n/g, '\n').split('\n');
  }

  remaining(): string {
    return this.lines.slice(this.idx).join('\n');
  }

  private peek(): string | undefined {
    return this.lines[this.idx];
  }

  private advance(): string {
    return this.lines[this.idx++];
  }

  // Try to parse one top-level dump value starting at the current index.
  // Returns null if the current line can't open a dump value.
  parseValue(): DumpNode | null {
    // Skip blank lines and comment-like leftover prompts.
    while (this.peek() !== undefined && this.peek()!.trim() === '') {
      this.advance();
    }
    const line = this.peek();
    if (line === undefined) return null;
    const trimmed = line.trim();

    // Inline (single-line) array
    if (ARRAY_EMPTY_RE.test(trimmed)) {
      this.advance();
      return { kind: 'array', count: 0, items: [] };
    }

    // Inline object: `Class {#42}` or `Class {#42 …}`
    const inlineObj = trimmed.match(OBJECT_INLINE_RE);
    if (inlineObj?.groups) {
      this.advance();
      return {
        kind: 'object',
        class: inlineObj.groups.class,
        id: inlineObj.groups.id ? Number(inlineObj.groups.id) : undefined,
        ref: true,
        props: []
      };
    }

    // Multi-line array
    const arrOpen = trimmed.match(ARRAY_OPEN_RE);
    if (arrOpen?.groups) {
      this.advance();
      const count = Number(arrOpen.groups.count);
      const items: ArrayItem[] = [];
      while (this.peek() !== undefined) {
        const cur = this.peek()!.trim();
        if (cur === ']') {
          this.advance();
          return { kind: 'array', count, items };
        }
        const item = this.parseArrayEntry();
        if (item) items.push(item);
        else this.advance();
      }
      return { kind: 'array', count, items };
    }

    // Multi-line object
    const objOpen = trimmed.match(OBJECT_OPEN_RE);
    if (objOpen?.groups) {
      this.advance();
      const props: Property[] = [];
      while (this.peek() !== undefined) {
        const cur = this.peek()!.trim();
        if (cur === '}') {
          this.advance();
          return {
            kind: 'object',
            class: objOpen.groups.class,
            id: objOpen.groups.id ? Number(objOpen.groups.id) : undefined,
            ref: false,
            props
          };
        }
        const prop = this.parseProperty();
        if (prop) props.push(prop);
        else this.advance();
      }
      return {
        kind: 'object',
        class: objOpen.groups.class,
        id: objOpen.groups.id ? Number(objOpen.groups.id) : undefined,
        ref: false,
        props
      };
    }

    // Top-level scalar (e.g. a bare `1`, `"hello"`, `null`). We accept
    // these so a script with multiple `dump()` calls produces one node
    // per dumped value, no matter what was dumped.
    if (isScalarLine(trimmed)) {
      this.advance();
      return { kind: 'scalar', type: classifyScalar(trimmed), value: trimmed };
    }

    return null;
  }

  private parseProperty(): Property | null {
    const line = this.peek();
    if (line === undefined) return null;
    const m = line.trim().match(PROP_RE);
    if (!m?.groups) return null;
    this.advance();
    const visibility = visibilitySymbol(m.groups.vis);
    const name = m.groups.name;
    const rest = m.groups.rest;
    return { name, visibility, value: this.parseInlineOrComplex(rest) };
  }

  private parseArrayEntry(): ArrayItem | null {
    const line = this.peek();
    if (line === undefined) return null;
    const m = line.trim().match(ENTRY_RE);
    if (!m?.groups) return null;
    this.advance();
    const rawKey = m.groups.key;
    const key = rawKey.startsWith('"') ? rawKey.slice(1, -1) : rawKey;
    const rest = m.groups.rest;
    return { key, value: this.parseInlineOrComplex(rest) };
  }

  // Given the text after `: ` (property) or `=> ` (array entry), decide if
  // it's a scalar that fits on one line, or the start of a nested complex
  // value whose body continues on subsequent lines.
  private parseInlineOrComplex(rest: string): DumpNode {
    const trimmed = rest.trim();

    if (ARRAY_EMPTY_RE.test(trimmed)) {
      return { kind: 'array', count: 0, items: [] };
    }
    const inlineObj = trimmed.match(OBJECT_INLINE_RE);
    if (inlineObj?.groups) {
      return {
        kind: 'object',
        class: inlineObj.groups.class,
        id: inlineObj.groups.id ? Number(inlineObj.groups.id) : undefined,
        ref: true,
        props: []
      };
    }

    // Multi-line array opening on this line: pretend the next iteration
    // starts at the array body. We need the parser to re-enter at this
    // line, but we already consumed it. Manufacture a synthetic restart
    // by pushing the rest back as the current line.
    const arrOpen = trimmed.match(ARRAY_OPEN_RE);
    if (arrOpen?.groups) {
      // Insert a synthetic line; parse, then continue.
      this.lines.splice(this.idx, 0, trimmed);
      const v = this.parseValue();
      if (v) return v;
    }

    const objOpen = trimmed.match(OBJECT_OPEN_RE);
    if (objOpen?.groups) {
      this.lines.splice(this.idx, 0, trimmed);
      const v = this.parseValue();
      if (v) return v;
    }

    // Plain scalar
    return { kind: 'scalar', type: classifyScalar(trimmed), value: trimmed };
  }
}

export function parseDump(text: string): ParseResult {
  const parser = new Parser(text);
  const nodes: DumpNode[] = [];
  // Skip leading blank lines.
  while (true) {
    const node = parser.parseValue();
    if (!node) break;
    nodes.push(node);
  }
  return {
    ok: nodes.length > 0,
    nodes,
    trailing: parser.remaining()
  };
}

// Quick check: does this text look like a Symfony var-dumper dump? Used
// by the UI to decide between tree rendering and plain <pre>.
export function looksLikeDump(text: string): boolean {
  const t = text.trimStart();
  return /^[A-Za-z_\\][\w\\.]* \{(?:#\d+)?/.test(t) || /^array:\d+ \[/.test(t);
}
