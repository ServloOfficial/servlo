<script lang="ts">
  /**
   * The one indicator of which mode this machine is in.
   *
   * It sits in the shell rather than a settings page on purpose. Production
   * mode decides whether a visitor sees a stack trace or a blank page, and
   * whether an edited file is picked up at all, so someone about to debug
   * either of those needs to know before they start rather than after.
   *
   * Both states are shown. A badge that only appears in production leaves the
   * other mode indistinguishable from "the badge is broken".
   */
  import { status } from '$stores/status';
  import { tooltip } from '$lib/tooltip';

  const live = $derived(Boolean($status?.production));
  const since = $derived($status?.production_since);

  const hint = $derived(
    live
      ? since
        ? `Production mode, on since ${new Date(since).toLocaleString()}. PHP errors are hidden from visitors and OPcache does not re-read changed files.`
        : 'Production mode. PHP errors are hidden from visitors and OPcache does not re-read changed files.'
      : 'Development mode. PHP errors are shown and OPcache re-reads changed files. Turn it on with: servlo production on'
  );
</script>

<span
  use:tooltip={hint}
  data-production={live ? 'true' : 'false'}
  class="shrink-0 inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium border {live
    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
    : 'border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400'}"
>
  <span class="w-1.5 h-1.5 rounded-full {live ? 'bg-emerald-500' : 'bg-amber-500'}"></span>
  {live ? 'Production' : 'Development'}
</span>
