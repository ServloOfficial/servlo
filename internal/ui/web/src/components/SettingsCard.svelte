<script lang="ts">
  import type { Snippet } from 'svelte';

  // A single settings section rendered as a card. The section supplies its own
  // header (title + status pill) and controls as children; this just provides the
  // consistent card shell shared across the System > servlo page.
  let { children }: { children: Snippet } = $props();
</script>

<div class="rounded-xl border border-gray-100 dark:border-servlo-border bg-white dark:bg-servlo-card p-4">
  {@render children()}
</div>
