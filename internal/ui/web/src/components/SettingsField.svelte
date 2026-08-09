<script lang="ts">
  import type { Snippet } from 'svelte';

  // One labelled control with its hint, the text half of a settings row. The
  // control itself is the caller's, so a select, an input and a path box all
  // read the same way without three copies of the same markup.
  interface Props {
    label: string;
    hint?: string;
    forId?: string;
    children: Snippet;
  }
  let { label, hint, forId, children }: Props = $props();
</script>

<div>
  <label class="text-xs font-medium text-gray-700 dark:text-gray-200" for={forId}>{label}</label>
  {#if hint}
    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{hint}</p>
  {/if}
  <div class="mt-2">
    {@render children()}
  </div>
</div>
