<script lang="ts">
  import Icon from '$components/Icon.svelte';
  import MonacoEditor from '$components/MonacoEditor.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import { formatBytes } from '$lib/bytes';
  import { openFileDeleteModal, openFilePermissionsModal } from '$stores/modals';
  import {
    breadcrumbs,
    isArchive,
    languageForFile,
    loadFileContent,
    loadFiles,
    parentOf,
    saveFileContent,
    unzipFile,
    uploadFile,
    type FileContent,
    type FileEntry,
    type FileListing
  } from '$stores/files';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let path = $state('');
  let listing = $state<FileListing | null>(null);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');

  // The open file, if any. Editing replaces the listing rather than sitting
  // beside it: a split view at this width leaves neither half usable.
  let open = $state<FileContent | null>(null);
  let buffer = $state('');
  let saving = $state(false);

  let uploading = $state(false);
  let uploadPercent = $state(0);
  let fileInput = $state<HTMLInputElement | undefined>();
  let busyPath = $state('');

  const dirty = $derived(!!open && buffer !== open.text);
  const crumbs = $derived(breadcrumbs(path));

  async function refresh() {
    const domain = site.domain;
    const at = path;
    loading = true;
    error = '';
    const res = await loadFiles(domain, at);
    if (site.domain !== domain || path !== at) return;
    loading = false;
    if (res.error) {
      error = res.error;
      listing = null;
      return;
    }
    listing = res;
  }

  // Reload whenever the site or the directory changes. Closing the editor is
  // part of navigating: the file that was open belongs to the old directory.
  $effect(() => {
    void site.domain;
    void path;
    open = null;
    void refresh();
  });

  function go(next: string) {
    notice = '';
    path = next;
  }

  async function openFile(entry: FileEntry) {
    notice = '';
    busyPath = entry.path;
    try {
      const content = await loadFileContent(site.domain, entry.path);
      if (content.error) {
        error = content.error;
        return;
      }
      open = content;
      buffer = content.text;
    } finally {
      busyPath = '';
    }
  }

  async function save() {
    if (!open) return;
    saving = true;
    error = '';
    const res = await saveFileContent(site.domain, open.path, buffer);
    saving = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    open = { ...open, text: buffer };
  }

  async function extract(entry: FileEntry) {
    notice = '';
    error = '';
    busyPath = entry.path;
    try {
      const res = await unzipFile(site.domain, entry.path);
      if (!res.ok) {
        error = res.error || m.common_failed();
        return;
      }
      notice = m.files_extracted({ n: res.files ?? 0 });
      await refresh();
    } finally {
      busyPath = '';
    }
  }

  function remove(entry: FileEntry) {
    openFileDeleteModal({
      domain: site.domain,
      path: entry.path,
      name: entry.name,
      dir: entry.dir,
      onDeleted: () => void refresh()
    });
  }

  function fixPermissions() {
    openFilePermissionsModal({ domain: site.domain, onApplied: () => void refresh() });
  }

  async function onUpload(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    uploading = true;
    uploadPercent = 0;
    error = '';
    notice = '';
    try {
      const res = await uploadFile(site.domain, path, file, (p) => (uploadPercent = p));
      if (!res.ok) {
        error = res.error || m.common_failed();
        return;
      }
      await refresh();
    } finally {
      uploading = false;
      input.value = '';
    }
  }

  function modified(unix: number): string {
    if (!unix) return '';
    return new Date(unix * 1000).toLocaleString();
  }
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden">
  <!-- Always visible, never behind a toggle: this edits a site that is serving
       traffic right now, and the moment that warning is dismissible is the
       moment it stops being read. -->
  <div
    class="flex items-start gap-2 px-3 py-2 bg-amber-50 dark:bg-amber-900/15 border-b border-amber-200 dark:border-amber-900/40 shrink-0"
  >
    <Icon name="alert" class="w-3.5 h-3.5 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
    <p class="text-xs text-amber-700 dark:text-amber-300 leading-relaxed">
      {m.files_liveWarning()}
    </p>
  </div>

  <div
    class="flex items-center justify-between gap-2 bg-gray-50 dark:bg-white/3 px-3 py-1.5 border-b border-gray-200 dark:border-servlo-border shrink-0"
  >
    <div class="flex items-center gap-1 min-w-0 text-xs">
      <button
        type="button"
        class="text-gray-600 dark:text-gray-300 hover:text-servlo-red truncate"
        title={listing?.root}
        onclick={() => go('')}>{m.files_root()}</button
      >
      {#each crumbs as crumb (crumb.path)}
        <span class="text-gray-300 dark:text-gray-700">/</span>
        <button
          type="button"
          class="text-gray-600 dark:text-gray-300 hover:text-servlo-red truncate max-w-40"
          onclick={() => go(crumb.path)}>{crumb.name}</button
        >
      {/each}
    </div>
    <div class="flex items-center gap-2 shrink-0">
      <DetailButton onclick={fixPermissions}>{m.files_fixPermissions()}</DetailButton>
      <DetailButton
        tone="primary"
        loading={uploading}
        disabled={uploading}
        onclick={() => fileInput?.click()}
      >
        {uploading ? m.files_uploading() : m.files_upload()}
      </DetailButton>
      <input
        bind:this={fileInput}
        type="file"
        class="hidden"
        onchange={onUpload}
      />
    </div>
  </div>

  {#if uploading}
    <div class="h-0.5 bg-gray-200 dark:bg-white/10 shrink-0">
      <div class="h-full bg-servlo-red transition-all" style="width: {uploadPercent}%"></div>
    </div>
  {/if}

  {#if notice}
    <p class="text-xs text-emerald-600 dark:text-emerald-400 px-3 py-1.5 border-b border-gray-100 dark:border-servlo-border shrink-0">
      {notice}
    </p>
  {/if}
  {#if error}
    <p class="text-xs text-red-500 dark:text-red-400 px-3 py-1.5 border-b border-red-200 dark:border-red-900/40 shrink-0">
      {error}
    </p>
  {/if}

  {#if open}
    <div
      class="flex items-center justify-between gap-2 px-3 py-1.5 border-b border-gray-200 dark:border-servlo-border shrink-0"
    >
      <div class="flex items-center gap-2 min-w-0">
        <span class="text-xs font-mono text-gray-700 dark:text-gray-200 truncate">{open.path}</span>
        <span class="text-[10px] text-gray-400 dark:text-gray-600 tabular-nums shrink-0"
          >{formatBytes(open.size)} · {open.mode}</span
        >
        {#if dirty}
          <span class="text-[10px] font-medium text-amber-600 dark:text-amber-400 shrink-0"
            >{m.files_unsaved()}</span
          >
        {/if}
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <DetailButton onclick={() => (open = null)} disabled={saving}>
          {m.common_cancel()}
        </DetailButton>
        <DetailButton
          tone="primary"
          onclick={save}
          loading={saving}
          disabled={saving || !dirty || open.binary || open.truncated}
        >
          {m.common_save()}
        </DetailButton>
      </div>
    </div>

    <div class="flex-1 min-h-0 overflow-hidden bg-gray-50 dark:bg-black/40">
      {#if open.binary}
        <p class="text-xs text-gray-500 dark:text-gray-400 px-3 py-2.5">{m.files_binary()}</p>
      {:else}
        {#if open.truncated}
          <p class="text-xs text-amber-600 dark:text-amber-400 px-3 py-1.5">
            {m.files_fileTruncated()}
          </p>
        {/if}
        <div class="h-full min-h-64">
          <MonacoEditor
            bind:value={buffer}
            readOnly={open.truncated}
            language={languageForFile(open.path)}
          />
        </div>
      {/if}
    </div>
  {:else}
    <div class="flex-1 min-h-0 overflow-y-auto">
      {#if loading}
        <p class="text-xs text-gray-400 px-3 py-2.5">{m.common_loading()}</p>
      {:else if !listing}
        <p class="text-xs text-gray-400 px-3 py-2.5">{m.files_loadFailed()}</p>
      {:else if listing.entries.length === 0 && !path}
        <EmptyState title={m.files_empty()} />
      {:else}
        <table class="w-full text-xs">
          <thead
            class="sticky top-0 bg-gray-50 dark:bg-servlo-panel text-gray-500 dark:text-gray-400"
          >
            <tr class="border-b border-gray-200 dark:border-servlo-border">
              <th class="text-left font-medium px-3 py-1.5">{m.files_colName()}</th>
              <th class="text-right font-medium px-3 py-1.5 w-24">{m.files_colSize()}</th>
              <th class="text-right font-medium px-3 py-1.5 w-20 hidden sm:table-cell">
                {m.files_colMode()}
              </th>
              <th class="text-right font-medium px-3 py-1.5 w-44 hidden md:table-cell">
                {m.files_colModified()}
              </th>
              <th class="px-3 py-1.5 w-52"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-servlo-border">
            {#if path}
              <tr class="hover:bg-gray-50 dark:hover:bg-white/3">
                <td class="px-3 py-1.5" colspan="5">
                  <button
                    type="button"
                    class="flex items-center gap-2 text-gray-600 dark:text-gray-300 hover:text-servlo-red"
                    onclick={() => go(parentOf(path))}
                  >
                    <Icon name="back" class="w-3.5 h-3.5" />
                    {m.files_up()}
                  </button>
                </td>
              </tr>
            {/if}
            {#each listing.entries as entry (entry.path)}
              <tr class="hover:bg-gray-50 dark:hover:bg-white/3">
                <td class="px-3 py-1.5 min-w-0">
                  <div class="flex items-center gap-2 min-w-0">
                    <Icon
                      name={entry.dir ? 'folder' : 'file'}
                      class="w-3.5 h-3.5 shrink-0 {entry.dir
                        ? 'text-servlo-red/70'
                        : 'text-gray-400 dark:text-gray-600'}"
                    />
                    {#if entry.dir && !entry.escapes}
                      <button
                        type="button"
                        class="truncate text-gray-800 dark:text-gray-100 hover:text-servlo-red"
                        onclick={() => go(entry.path)}>{entry.name}</button
                      >
                    {:else}
                      <span class="truncate text-gray-800 dark:text-gray-100">{entry.name}</span>
                    {/if}
                    {#if entry.escapes}
                      <span
                        class="shrink-0 text-[10px] px-1 py-0.5 rounded-sm bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300"
                        >{m.files_linkOutside()}</span
                      >
                    {/if}
                  </div>
                </td>
                <td class="px-3 py-1.5 text-right tabular-nums text-gray-500 dark:text-gray-400">
                  {entry.dir || entry.escapes ? '' : formatBytes(entry.size)}
                </td>
                <td
                  class="px-3 py-1.5 text-right font-mono text-gray-400 dark:text-gray-600 hidden sm:table-cell"
                  >{entry.mode}</td
                >
                <td
                  class="px-3 py-1.5 text-right tabular-nums text-gray-400 dark:text-gray-600 hidden md:table-cell"
                  >{modified(entry.modified)}</td
                >
                <td class="px-3 py-1.5">
                  <div class="flex items-center justify-end gap-1.5">
                    {#if !entry.dir && !entry.escapes}
                      {#if isArchive(entry.name)}
                        <DetailButton
                          onclick={() => extract(entry)}
                          loading={busyPath === entry.path}
                          disabled={busyPath === entry.path}>{m.files_extract()}</DetailButton
                        >
                      {:else}
                        <DetailButton
                          onclick={() => openFile(entry)}
                          loading={busyPath === entry.path}
                          disabled={busyPath === entry.path}>{m.files_edit()}</DetailButton
                        >
                      {/if}
                    {/if}
                    <DetailButton onclick={() => remove(entry)}>{m.files_delete()}</DetailButton>
                  </div>
                </td>
              </tr>
            {/each}
            {#if listing.entries.length === 0 && path}
              <tr>
                <td class="px-3 py-2.5 text-gray-400" colspan="5">{m.files_empty()}</td>
              </tr>
            {/if}
          </tbody>
        </table>
        {#if listing.truncated}
          <p class="text-xs text-amber-600 dark:text-amber-400 px-3 py-1.5">
            {m.files_truncated({ n: listing.entries.length })}
          </p>
        {/if}
      {/if}
    </div>
  {/if}
</div>
