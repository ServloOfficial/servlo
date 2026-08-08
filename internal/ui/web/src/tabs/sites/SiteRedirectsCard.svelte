<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import type { Redirect } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  // The redirect half of the nginx settings, its own card because a moved
  // domain and a cache window are unrelated decisions and reading them as one
  // form invites saving one while meaning the other. The parent owns the save,
  // so both cards go through one request and one vhost write.
  interface Props {
    redirectTo: string;
    redirectPermanent: boolean;
    redirects: Redirect[];
    saving: boolean;
    saved: boolean;
    error: string;
    onSave: () => void;
  }
  let {
    redirectTo = $bindable(),
    redirectPermanent = $bindable(),
    redirects = $bindable(),
    saving,
    saved,
    error,
    onSave
  }: Props = $props();
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
    {m.sites_redirects_title()}
  </h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
    {m.sites_redirects_desc()}
  </p>

  <div class="mt-4">
    <label class="text-xs font-medium text-gray-700 dark:text-gray-200" for="redirect-whole">
      {m.sites_redirects_whole()}
    </label>
    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
      {m.sites_redirects_wholeHint()}
    </p>
    <input
      id="redirect-whole"
      type="text"
      placeholder={m.sites_redirects_wholePlaceholder()}
      bind:value={redirectTo}
      class="mt-2 w-full px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
    />
    <label class="mt-2 flex items-center gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
      <input type="checkbox" bind:checked={redirectPermanent} />
      {m.sites_redirects_permanent()}
    </label>
  </div>

  <div class="mt-5">
    <div class="text-xs font-medium text-gray-700 dark:text-gray-200">
      {m.sites_redirects_rules()}
    </div>
    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
      {m.sites_redirects_rulesHint()}
    </p>
    <div class="mt-2 space-y-2">
      {#each redirects as rule, i (i)}
        <div class="flex items-center gap-2">
          <input
            type="text"
            aria-label="{m.sites_redirects_from()} {i + 1}"
            placeholder={m.sites_redirects_from()}
            bind:value={rule.from}
            class="flex-1 px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
          />
          <input
            type="text"
            aria-label="{m.sites_redirects_to()} {i + 1}"
            placeholder={m.sites_redirects_to()}
            bind:value={rule.to}
            class="flex-1 px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
          />
          <label class="flex items-center gap-1 text-[11px] text-gray-500 dark:text-gray-400">
            <input
              type="checkbox"
              aria-label="{m.sites_redirects_permanent()} {i + 1}"
              bind:checked={rule.permanent}
            />
            301
          </label>
          <button
            type="button"
            aria-label="{m.sites_redirects_remove()} {i + 1}"
            onclick={() => (redirects = redirects.filter((_, j) => j !== i))}
            class="px-2 py-1 rounded-md text-xs text-gray-400 hover:text-servlo-red transition-colors"
          >
            &times;
          </button>
        </div>
      {/each}
    </div>
    <button
      type="button"
      onclick={() => (redirects = [...redirects, { from: '', to: '', permanent: false }])}
      class="mt-2 text-xs text-servlo-red hover:text-servlo-redhov transition-colors"
    >
      {m.sites_redirects_add()}
    </button>
  </div>

  <div class="mt-4 flex items-center gap-3">
    <button
      type="button"
      onclick={onSave}
      disabled={saving}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
    >
      {saving ? m.sites_phpSettings_saving() : m.sites_redirects_save()}
    </button>
    {#if saved}
      <span class="text-xs text-green-600 dark:text-green-400">{m.sites_phpSettings_saved()}</span>
    {/if}
  </div>

  {#if error}
    <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap">{error}</p>
  {/if}
</SettingsCard>
