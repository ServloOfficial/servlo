<script lang="ts">
  import DetailButton from './DetailButton.svelte';
  import { m } from '../paraglide/messages.js';

  // A block of commands for a person to run in a terminal, with one button to
  // take the lot.
  //
  // It exists as a component because servlo keeps producing these: anything
  // needing root is printed rather than run, and a copy button typed out again
  // in each view is where "Copy" turns into "Copied" in one place and not the
  // next.

  interface Props {
    commands: string[];
    title?: string;
    /** Shown between the heading and the commands, for the why. */
    intro?: string;
  }
  let { commands, title, intro }: Props = $props();

  let copied = $state(false);
  const text = $derived(commands.join('\n'));

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      /* no clipboard permission */
    }
  }
</script>

{#if commands.length > 0}
  <div class="rounded-sm border border-gray-200 dark:border-servlo-border">
    <div
      class="flex items-center justify-between gap-2 px-3 py-2 border-b border-gray-100 dark:border-servlo-border"
    >
      <h4 class="text-[11px] font-semibold text-gray-600 dark:text-gray-300">
        {title ?? m.command_runThese()}
      </h4>
      <DetailButton onclick={copy}>{copied ? m.common_copied() : m.common_copy()}</DetailButton>
    </div>
    {#if intro}
      <p class="px-3 pt-2 text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{intro}</p>
    {/if}
    <pre
      class="px-3 py-2 text-[11px] font-mono text-gray-700 dark:text-gray-300 overflow-x-auto">{text}</pre>
  </div>
{/if}
