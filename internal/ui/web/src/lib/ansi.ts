// Minimal SGR (Select Graphic Rendition) parser. Converts ANSI escape
// sequences from CLI tools into safe HTML span tags. Handles:
//   - foreground 30-37 / bright 90-97 / default 39
//   - background 40-47 / bright 100-107 / default 49
//   - bold (1) and reset (0 / 22)
//   - 256-color foreground (38;5;N) — falls back to closest 8-color
//
// Skips cursor positioning, line clear, and other non-SGR sequences (they're
// noise in a non-interactive replay). Strips them rather than letting them
// reach the DOM.

const FG: Record<number, string> = {
  30: 'var(--ansi-black, #555)',
  31: 'var(--ansi-red, #ff5555)',
  32: 'var(--ansi-green, #50fa7b)',
  33: 'var(--ansi-yellow, #f1fa8c)',
  34: 'var(--ansi-blue, #6272ff)',
  35: 'var(--ansi-magenta, #ff79c6)',
  36: 'var(--ansi-cyan, #8be9fd)',
  37: 'var(--ansi-white, #f8f8f2)',
  90: 'var(--ansi-bright-black, #6272a4)',
  91: 'var(--ansi-bright-red, #ff6e6e)',
  92: 'var(--ansi-bright-green, #69ff94)',
  93: 'var(--ansi-bright-yellow, #ffffa5)',
  94: 'var(--ansi-bright-blue, #d6acff)',
  95: 'var(--ansi-bright-magenta, #ff92df)',
  96: 'var(--ansi-bright-cyan, #a4ffff)',
  97: 'var(--ansi-bright-white, #ffffff)'
};

function htmlEscape(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

interface SpanState {
  fg?: string;
  bold?: boolean;
}

function spanStyle(s: SpanState): string {
  const parts: string[] = [];
  if (s.fg) parts.push('color:' + s.fg);
  if (s.bold) parts.push('font-weight:600');
  return parts.join(';');
}

export function ansiToHtml(text: string): string {
  // ESC[...m for SGR. We ignore non-m escapes (cursor moves, etc.) by
  // matching ESC[ followed by any param chars then a terminator letter.
  const sgrRe = /\x1b\[([\d;]*)m/g;
  const otherEscRe = /\x1b\[[\d;]*[A-Za-z]/g;
  // First scrub non-SGR escapes by replacing them with empty string.
  // Then walk SGR matches.
  let cleaned = text.replace(otherEscRe, (m) => (m.endsWith('m') ? m : ''));
  // Also drop other common ESC sequences we don't model: ESC] OSC, ESC( charset.
  cleaned = cleaned.replace(/\x1b\][^\x07\x1b]*(\x07|\x1b\\)/g, '').replace(/\x1b[()][\dA-Za-z]/g, '');

  let out = '';
  let last = 0;
  const state: SpanState = {};
  let openSpan = false;

  const writeChunk = (chunk: string) => {
    if (!chunk) return;
    const escaped = htmlEscape(chunk);
    if (openSpan) {
      out += escaped;
    } else {
      const style = spanStyle(state);
      if (style) {
        out += '<span style="' + style + '">' + escaped;
        openSpan = true;
      } else {
        out += escaped;
      }
    }
  };

  const closeSpan = () => {
    if (openSpan) {
      out += '</span>';
      openSpan = false;
    }
  };

  let m: RegExpExecArray | null;
  while ((m = sgrRe.exec(cleaned)) !== null) {
    writeChunk(cleaned.slice(last, m.index));
    last = m.index + m[0].length;

    const params = m[1].split(';').filter(Boolean).map(Number);
    if (params.length === 0) params.push(0);

    for (let i = 0; i < params.length; i++) {
      const p = params[i];
      if (p === 0) {
        closeSpan();
        state.fg = undefined;
        state.bold = false;
      } else if (p === 1) {
        closeSpan();
        state.bold = true;
      } else if (p === 22) {
        closeSpan();
        state.bold = false;
      } else if (p === 39) {
        closeSpan();
        state.fg = undefined;
      } else if (FG[p]) {
        closeSpan();
        state.fg = FG[p];
      } else if (p === 38 && params[i + 1] === 5) {
        // 256-color: round to nearest 8-color base; good enough for readability.
        const idx = params[i + 2] ?? 0;
        const base = idx >= 8 && idx <= 15 ? 90 + (idx - 8) : 30 + (idx % 8);
        closeSpan();
        state.fg = FG[base];
        i += 2;
      }
      // Ignore unrecognized codes (background, underline, italic, etc.) silently.
    }
  }
  writeChunk(cleaned.slice(last));
  closeSpan();
  return out;
}
