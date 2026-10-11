<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type MappingOverride } from '../lib/api';

  let overrides: MappingOverride[] = [];
  let loading = true;
  let saving = false;
  let deletingId: number | null = null;
  let errorMessage = '';
  let successMessage = '';
  let searchQuery = '';

  // New / Edit override form
  let showForm = false;
  let formAniListId = '';
  let formMediaType = 'SERIES';
  let formTvdbId = '';
  let formTmdbId = '';
  let formSeasons = '1';
  let formTitleOverride = '';

  async function loadOverrides() {
    loading = true;
    errorMessage = '';
    try {
      overrides = await api.getOverrides();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to load mapping overrides';
    } finally {
      loading = false;
    }
  }

  function resetForm() {
    formAniListId = '';
    formMediaType = 'SERIES';
    formTvdbId = '';
    formTmdbId = '';
    formSeasons = '1';
    formTitleOverride = '';
    showForm = false;
  }

  function startEdit(o: MappingOverride) {
    formAniListId = String(o.anilistId);
    formMediaType = o.mediaType || 'SERIES';
    formTvdbId = o.tvdbId ? String(o.tvdbId) : '';
    formTmdbId = o.tmdbId ? String(o.tmdbId) : '';
    formSeasons = o.seasons || '1';
    formTitleOverride = o.titleOverride || '';
    showForm = true;
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  async function handleSaveOverride() {
    const anilistIdNum = parseInt(formAniListId.trim(), 10);
    if (!anilistIdNum || anilistIdNum <= 0) {
      errorMessage = 'Please enter a valid positive AniList ID.';
      return;
    }

    const tvdbNum = formTvdbId.trim() ? parseInt(formTvdbId.trim(), 10) : undefined;
    const tmdbNum = formTmdbId.trim() ? parseInt(formTmdbId.trim(), 10) : undefined;

    if (!tvdbNum && !tmdbNum) {
      errorMessage = 'At least one TVDB ID or TMDb ID is required.';
      return;
    }

    saving = true;
    errorMessage = '';
    successMessage = '';

    try {
      await api.createOverride({
        anilistId: anilistIdNum,
        mediaType: formMediaType,
        tvdbId: tvdbNum,
        tmdbId: tmdbNum,
        seasons: formMediaType === 'SERIES' ? formSeasons.trim() || '1' : '1',
        titleOverride: formTitleOverride.trim() || undefined,
      });

      successMessage = `Saved mapping override for AniList #${anilistIdNum}.`;
      resetForm();
      await loadOverrides();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to save mapping override';
    } finally {
      saving = false;
    }
  }

  async function handleDelete(anilistId: number) {
    if (!confirm(`Delete mapping override for AniList ID ${anilistId}?`)) {
      return;
    }

    deletingId = anilistId;
    errorMessage = '';
    successMessage = '';

    try {
      await api.deleteOverride(anilistId);
      successMessage = `Deleted mapping override for AniList #${anilistId}.`;
      await loadOverrides();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to delete mapping override';
    } finally {
      deletingId = null;
    }
  }

  $: filteredOverrides = overrides.filter((o) => {
    if (!searchQuery.trim()) return true;
    const q = searchQuery.toLowerCase();
    return (
      String(o.anilistId).includes(q) ||
      (o.titleOverride && o.titleOverride.toLowerCase().includes(q)) ||
      (o.tvdbId && String(o.tvdbId).includes(q)) ||
      (o.tmdbId && String(o.tmdbId).includes(q))
    );
  });

  onMount(loadOverrides);
</script>

<div class="space-y-6">
  <!-- Top header -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-[#24374e]">
    <div>
      <h2 class="text-xl font-bold text-white tracking-tight">Manual Mapping Overrides</h2>
      <p class="text-xs text-slate-400 mt-0.5">
        Custom ID mappings that take highest priority during metadata resolution and bypass normal matching.
      </p>
    </div>

    <div class="flex items-center gap-2">
      <button
        type="button"
        on:click={() => (showForm = !showForm)}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 text-slate-950 transition shadow-sm"
      >
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" />
        </svg>
        <span>{showForm ? 'Close Form' : 'Add Override'}</span>
      </button>

      <button
        type="button"
        on:click={loadOverrides}
        class="p-1.5 rounded-lg border border-[#24374e] text-slate-400 hover:text-white hover:bg-[#19273a] transition"
        title="Refresh overrides"
      >
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
      </button>
    </div>
  </div>

  {#if errorMessage}
    <div class="p-3 rounded-lg bg-rose-950/60 border border-rose-800 text-rose-300 text-sm">
      {errorMessage}
    </div>
  {/if}

  {#if successMessage}
    <div class="p-3 rounded-lg bg-emerald-950/60 border border-emerald-800 text-emerald-300 text-sm">
      {successMessage}
    </div>
  {/if}

  <!-- Add / Edit Override Form Card -->
  {#if showForm}
    <div class="p-5 rounded-xl bg-[#152232] border border-[#24374e] shadow-xl space-y-4">
      <div class="flex items-center justify-between pb-2 border-b border-[#24374e]">
        <h3 class="text-sm font-semibold text-white">
          {formAniListId ? `Create or Edit Override for AniList #${formAniListId}` : 'Add New Mapping Override'}
        </h3>
        <button type="button" on:click={resetForm} class="text-slate-400 hover:text-white text-xs">
          Cancel
        </button>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3">
        <div class="space-y-1">
          <label for="override-anilist-id" class="text-xs font-medium text-slate-300">AniList ID *</label>
          <input
            id="override-anilist-id"
            type="number"
            bind:value={formAniListId}
            placeholder="e.g. 16498"
            class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
          />
        </div>

        <div class="space-y-1">
          <label for="override-media-type" class="text-xs font-medium text-slate-300">Media Type *</label>
          <select
            id="override-media-type"
            bind:value={formMediaType}
            class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 focus:outline-none focus:border-sky-500"
          >
            <option value="SERIES">SERIES (Sonarr)</option>
            <option value="MOVIE">MOVIE (Radarr)</option>
          </select>
        </div>

        <div class="space-y-1">
          <label for="override-tvdb-id" class="text-xs font-medium text-slate-300">TVDB ID</label>
          <input
            id="override-tvdb-id"
            type="number"
            bind:value={formTvdbId}
            placeholder="e.g. 80348"
            class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
          />
        </div>

        <div class="space-y-1">
          <label for="override-tmdb-id" class="text-xs font-medium text-slate-300">TMDb ID</label>
          <input
            id="override-tmdb-id"
            type="number"
            bind:value={formTmdbId}
            placeholder="e.g. 1429"
            class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
          />
        </div>

        {#if formMediaType === 'SERIES'}
          <div class="space-y-1">
            <label for="override-seasons" class="text-xs font-medium text-slate-300">Seasons Mapping</label>
            <input
              id="override-seasons"
              type="text"
              bind:value={formSeasons}
              placeholder="e.g. 1 or 1,2"
              class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>
        {/if}

        <div class="space-y-1 sm:col-span-2">
          <label for="override-title" class="text-xs font-medium text-slate-300">Title Override (Optional)</label>
          <input
            id="override-title"
            type="text"
            bind:value={formTitleOverride}
            placeholder="Custom series title"
            class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
          />
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-2 border-t border-[#24374e]">
        <button
          type="button"
          on:click={resetForm}
          class="px-3 py-1.5 text-xs font-medium rounded-lg border border-[#24374e] text-slate-400 hover:text-white"
        >
          Cancel
        </button>
        <button
          type="button"
          on:click={handleSaveOverride}
          disabled={saving}
          class="px-4 py-1.5 text-xs font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 text-slate-950 disabled:opacity-50 transition shadow-sm"
        >
          {saving ? 'Saving...' : 'Save Override'}
        </button>
      </div>
    </div>
  {/if}

  <!-- Search Filter -->
  <div class="w-full sm:w-72">
    <input
      type="text"
      bind:value={searchQuery}
      placeholder="Search overrides by title or ID..."
      class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#152232] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
    />
  </div>

  <!-- Overrides Table -->
  {#if loading}
    <div class="p-12 text-center text-slate-400">
      <svg class="w-6 h-6 animate-spin mx-auto text-sky-400 mb-2" viewBox="0 0 24 24" fill="none">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
      </svg>
      <span class="text-xs">Loading overrides...</span>
    </div>
  {:else if filteredOverrides.length === 0}
    <div class="p-12 text-center rounded-xl bg-[#152232] border border-[#24374e] text-slate-400 space-y-2">
      <div class="text-slate-300 font-medium text-sm">No mapping overrides found</div>
      <p class="text-xs text-slate-500 max-w-md mx-auto">
        When an unmapped anime title is resolved in the Review Queue or added manually, the rule is stored here.
      </p>
    </div>
  {:else}
    <div class="overflow-x-auto rounded-xl border border-[#24374e] bg-[#152232]">
      <table class="w-full text-left text-xs text-slate-300 divide-y divide-[#24374e]">
        <thead class="bg-[#19273a] text-slate-400 uppercase font-semibold tracking-wider text-[11px]">
          <tr>
            <th class="px-4 py-3">AniList ID</th>
            <th class="px-4 py-3">Type</th>
            <th class="px-4 py-3">TVDB ID</th>
            <th class="px-4 py-3">TMDb ID</th>
            <th class="px-4 py-3">Seasons</th>
            <th class="px-4 py-3">Title Override</th>
            <th class="px-4 py-3">Updated</th>
            <th class="px-4 py-3 text-right">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#24374e]/60">
          {#each filteredOverrides as o (o.anilistId)}
            <tr class="hover:bg-[#19273a]/60 transition">
              <td class="px-4 py-3 font-mono">
                <a
                  href={`https://anilist.co/anime/${o.anilistId}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-sky-400 hover:underline font-semibold"
                >
                  #{o.anilistId} ↗
                </a>
              </td>

              <td class="px-4 py-3 whitespace-nowrap">
                <span
                  class="px-2 py-0.5 rounded text-[10px] font-bold {o.mediaType === 'SERIES' ? 'bg-sky-950 border border-sky-800 text-sky-300' : 'bg-emerald-950 border border-emerald-800 text-emerald-300'}"
                >
                  {o.mediaType}
                </span>
              </td>

              <td class="px-4 py-3 font-mono text-slate-300">{o.tvdbId ?? '-'}</td>
              <td class="px-4 py-3 font-mono text-slate-300">{o.tmdbId ?? '-'}</td>
              <td class="px-4 py-3 font-mono text-slate-300">{o.seasons || '1'}</td>
              <td class="px-4 py-3 font-medium text-white">{o.titleOverride || '-'}</td>
              <td class="px-4 py-3 text-slate-400 text-[11px] whitespace-nowrap">
                {new Date(o.updatedAt).toLocaleDateString()}
              </td>

              <td class="px-4 py-3 whitespace-nowrap text-right">
                <div class="inline-flex items-center gap-1.5">
                  <button
                    type="button"
                    on:click={() => startEdit(o)}
                    class="px-2 py-1 text-[11px] rounded border border-[#24374e] text-slate-300 hover:bg-[#19273a] transition"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    on:click={() => handleDelete(o.anilistId)}
                    disabled={deletingId === o.anilistId}
                    class="px-2 py-1 text-[11px] rounded bg-rose-500/10 text-rose-300 border border-rose-500/30 hover:bg-rose-500 hover:text-white transition disabled:opacity-50"
                  >
                    {deletingId === o.anilistId ? 'Deleting...' : 'Delete'}
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
