/** The prefix every container and unit servlo creates carries. */
export const UNIT_PREFIX = 'servlo-';

/**
 * The part of a container's name that is not servlo saying so.
 *
 * Measured from the prefix rather than counted by hand: this read
 * `n.slice(5)` against a seven-character prefix for the whole life of the
 * rename, so the dashboard listed o-panel, o-mysql and o-php84-fpm and no test
 * had an opinion about it.
 */
export function shortContainerName(name: string): string {
  return name.startsWith(UNIT_PREFIX) ? name.slice(UNIT_PREFIX.length) : name;
}
