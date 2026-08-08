<script lang="ts">
  import { m } from '../../paraglide/messages.js';

  // One labelled number box with its unit and the directives it writes. A
  // component from the start rather than three copies of the markup, because
  // the three fields differ only in their words.
  interface Props {
    label: string;
    hint: string;
    unit: string;
    max?: number;
    value: number | null;
  }
  let { label, hint, unit, max, value = $bindable() }: Props = $props();
</script>

<div>
  <div class="flex items-baseline justify-between gap-3">
    <label class="text-xs font-medium text-gray-700 dark:text-gray-200" for={label}>{label}</label>
    <div class="flex items-center gap-1.5">
      <input
        id={label}
        type="number"
        min="0"
        {max}
        placeholder={m.sites_phpSettings_default()}
        bind:value
        class="w-28 px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-right text-gray-700 dark:text-gray-200"
      />
      <span class="text-xs text-gray-400 w-14">{unit}</span>
    </div>
  </div>
  <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{hint}</p>
</div>
