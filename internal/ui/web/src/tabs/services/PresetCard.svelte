<script lang="ts">
  import DetailButton from '$components/DetailButton.svelte';
  import Icon from '$components/Icon.svelte';
  import { dashboardIconSvg } from '$lib/dashboardIcons';
  import { categoryOf, type CategoryKey } from '$lib/presetCategories';
  import { installPresetAndOpen, presetAddLabel, type Preset } from '$stores/presets';
  import { serviceLabel } from '$stores/services';

  // Per-category icon tints. Full static class strings so Tailwind keeps them.
  const ICON_TINT: Record<CategoryKey, string> = {
    databases: 'bg-indigo-50 text-indigo-600 dark:bg-indigo-500/10 dark:text-indigo-400',
    cache: 'bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-400',
    messaging: 'bg-violet-50 text-violet-600 dark:bg-violet-500/10 dark:text-violet-400',
    search: 'bg-sky-50 text-sky-600 dark:bg-sky-500/10 dark:text-sky-400',
    mail: 'bg-rose-50 text-rose-600 dark:bg-rose-500/10 dark:text-rose-400',
    admin: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400',
    storage: 'bg-cyan-50 text-cyan-600 dark:bg-cyan-500/10 dark:text-cyan-400',
    testing: 'bg-fuchsia-50 text-fuchsia-600 dark:bg-fuchsia-500/10 dark:text-fuchsia-400',
    other: 'bg-gray-100 text-gray-500 dark:bg-white/5 dark:text-gray-400'
  };

  interface Props {
    preset: Preset;
    category?: CategoryKey;
  }
  let { preset, category }: Props = $props();

  const tint = $derived(ICON_TINT[category ?? categoryOf(preset.name)]);

  async function add() {
    if (preset.installing) return;
    await installPresetAndOpen(preset);
  }
</script>

<div
  class="group flex items-center gap-3 rounded-xl border border-gray-200/80 dark:border-lerd-border bg-white dark:bg-lerd-card p-3 transition duration-150 hover:-translate-y-0.5 hover:shadow-lg hover:shadow-black/5 hover:border-gray-300 dark:hover:border-white/15"
>
  <span class="shrink-0 inline-flex items-center justify-center w-9 h-9 rounded-lg {tint} transition-transform group-hover:scale-105">
    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">{@html dashboardIconSvg(preset.name)}</svg>
  </span>
  <div class="min-w-0 flex-1">
    <div class="text-sm font-semibold text-gray-900 dark:text-white truncate" title={serviceLabel(preset.name)}>{serviceLabel(preset.name)}</div>
    {#if preset.error}
      <p class="text-[11px] leading-snug text-red-500 truncate" title={preset.error}>{preset.error}</p>
    {:else if preset.description}
      <p class="text-[11px] leading-snug text-gray-500 dark:text-gray-400 truncate" title={preset.description}>{preset.description}</p>
    {/if}
  </div>
  {#snippet plusIcon()}<Icon name="plus" class="w-4 h-4" />{/snippet}
  <div class="shrink-0">
    <DetailButton
      tone="secondary"
      onclick={add}
      disabled={Boolean(preset.installing)}
      loading={Boolean(preset.installing)}
      title={preset.installingMessage || presetAddLabel(preset)}
      icon={plusIcon}
    />
  </div>
</div>
