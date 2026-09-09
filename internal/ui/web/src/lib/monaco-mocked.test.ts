import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname, resolve, extname } from 'node:path';

// A test that renders a component reaching Monaco has to mock the loader.
//
// Monaco is five megabytes behind a dynamic import and cannot run in jsdom, so
// the two tests that render an editor directly have always mocked it. SiteEnvTab
// renders the tab those editors live inside and did not, which is a difference
// nobody chose: the component tree grew a level and the test one level up never
// learned what it was now pulling in. It loaded the real editor on every run and
// eventually lost the race between finishing that import and Vitest tearing the
// environment down, which is a CI failure with nothing wrong in the diff.
//
// Reachability rather than a list of component names, because the list is the
// thing that goes stale: a new card that embeds an editor puts every test above
// it back in the same position.
const SRC = resolve(__dirname, '..');
const MONACO = resolve(SRC, 'lib/monaco.ts');

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'paraglide') continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (['.ts', '.svelte'].includes(extname(p))) out.push(p);
  }
  return out;
}

// resolveImport turns an import specifier into a file in this tree, or null when
// it points at a package. Only the two aliases the app uses plus relative paths.
function resolveImport(from: string, spec: string): string | null {
  let base: string;
  if (spec.startsWith('.')) base = resolve(dirname(from), spec);
  else if (spec.startsWith('$lib/')) base = resolve(SRC, 'lib', spec.slice(5));
  else if (spec.startsWith('$components/')) base = resolve(SRC, 'components', spec.slice(12));
  else if (spec.startsWith('$stores/')) base = resolve(SRC, 'stores', spec.slice(8));
  else if (spec.startsWith('$tabs/')) base = resolve(SRC, 'tabs', spec.slice(6));
  else return null;
  for (const candidate of [base, base + '.ts', base + '.svelte']) {
    try {
      if (statSync(candidate).isFile()) return candidate;
    } catch {
      /* not this one */
    }
  }
  return null;
}

// Both spellings: `from '...'` and the side-effect `import '...'`.
const importsOf = (file: string): string[] =>
  [...readFileSync(file, 'utf8').matchAll(/(?:from|import)\s+['"]([^'"]+)['"]/g)]
    .map((m) => resolveImport(file, m[1]))
    .filter((p): p is string => p !== null);

const graph = new Map(walk(SRC).map((f) => [f, importsOf(f)] as const));

/**
 * Files that import monaco.ts directly or through any number of steps, ignoring
 * paths that pass through a file the caller has replaced.
 *
 * Replacing the component in the way is as good as replacing the loader, so a
 * test that stubs the tab holding the editor is not loading Monaco however many
 * levels below it the editor sits.
 */
function reachesMonaco(cut: Set<string> = new Set()): Set<string> {
  const reach = new Set<string>([MONACO]);
  for (let changed = true; changed; ) {
    changed = false;
    for (const [file, deps] of graph) {
      if (reach.has(file) || cut.has(file)) continue;
      if (deps.some((d) => reach.has(d))) {
        reach.add(file);
        changed = true;
      }
    }
  }
  reach.delete(MONACO);
  return reach;
}

/** The modules a test file replaces, resolved the same way its imports are. */
const mocksOf = (file: string): Set<string> => {
  const src = readFileSync(file, 'utf8');
  const out = new Set<string>();
  for (const m of src.matchAll(/vi\.mock\(\s*['"]([^'"]+)['"]/g)) {
    if (m[1] === '$lib/monaco') out.add(MONACO);
    const p = resolveImport(file, m[1]);
    if (p) out.add(p);
  }
  return out;
};

describe('Monaco in tests', () => {
  it('is mocked by every test that renders something reaching it', () => {
    const reach = reachesMonaco();
    const offenders: string[] = [];
    for (const file of walk(SRC).filter((f) => f.endsWith('.test.ts'))) {
      const mocked = mocksOf(file);
      if (mocked.has(MONACO)) continue;
      const left = mocked.size > 0 ? reachesMonaco(mocked) : reach;
      if (importsOf(file).some((d) => !mocked.has(d) && left.has(d))) {
        offenders.push(file.slice(SRC.length + 1));
      }
    }
    expect(offenders, 'these tests load the real Monaco into jsdom').toEqual([]);
  });
});
