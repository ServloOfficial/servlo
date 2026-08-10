<script lang="ts">
  import { version } from '$stores/version';
  import { m } from '../paraglide/messages.js';

  // "beta" is said in words rather than carried in the version string. A
  // -beta suffix is a thing every tag comparison and package manager has to
  // parse; this is a thing a person has to read, which is who it is for.
</script>

<span class="inline-flex items-baseline gap-1 pb-1">
  <span class="text-[10px] text-gray-400 dark:text-gray-600 font-mono">v{$version.current}</span>
  <span class="text-[9px] uppercase tracking-wide font-medium text-amber-600 dark:text-amber-500">{m.common_beta()}</span>
</span>
