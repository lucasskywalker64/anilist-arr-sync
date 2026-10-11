<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { api, type ReviewQueueItem } from '../lib/api';

  const dispatch = createEventDispatcher();

  export let reviewCount = 0;

  let items: ReviewQueueItem[] = [];
  let loading = true;
  let processingId: number | null = null;
  let errorMessage = '';
  let successMessage = '';
  let searchQuery = '';

  // Input states per review item
  interface FormState {
    idType: 'tvdb' | 'tmdb';
    resolvedId: string;
    seasons: string;
    titleOverride: string;
    ignoreReason: string;
    showIgnoreForm: boolean;
  }

  let formStates: Record<number, FormState> = {};

  async function loadReviewQueue() {
    loading = true;
    errorMessage = '';
    try {
      items = await api.getReviewQueue();
      reviewCount = items.length;
      dispatch('countChange', reviewCount);

      // Initialize form states
      const newStates: Record<number, FormState> = {};
      for (const item of items) {
        newStates[item.id] = formStates[item.id] || {
          idType: item.mediaType === 'MOVIE' ? 'tmdb' : 'tvdb',
          resolvedId: '',
          seasons: '1',
          titleOverride: item.titleEnglish || item.titleRomaji,
          ignoreReason: 'Unmapped title not available in collection',
          showIgnoreForm: false,
        };
      }
      formStates = newStates;
    } catch (err: any) {
      errorMessage = err.message || 'Failed to load review queue items';
    } finally {
      loading = false;
    }
  }

  function parseCandidates(jsonStr: string): any[] {
    try {
      if (!jsonStr) return [];
      const parsed = JSON.parse(jsonStr);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }

  function applyCandidate(itemId: number, candidate: any) {
    if (!formStates[itemId]) return;
    const isTvdb = !!candidate.tvdbId || (candidate.id && formStates[itemId].idType === 'tvdb');
    const idVal = candidate.tvdbId || candidate.tmdbId || candidate.id;
    if (idVal) {
      formStates[itemId].resolvedId = String(idVal);
      if (candidate.tvdbId) formStates[itemId].idType = 'tvdb';
      else if (candidate.tmdbId) formStates[itemId].idType = 'tmdb';
    }
    if (candidate.title) {
      formStates[itemId].titleOverride = candidate.title;
    }
    formStates = { ...formStates };
  }

  async function handleResolve(item: ReviewQueueItem) {
    const state = formStates[item.id];
    if (!state) return;

    const idNum = parseInt(state.resolvedId.trim(), 10);
    if (!idNum || idNum <= 0) {
      errorMessage = `Please enter a valid positive numeric ${state.idType.toUpperCase()} ID for "${item.titleRomaji}"`;
      return;
    }

    processingId = item.id;
    errorMessage = '';
    successMessage = '';

    try {
      await api.resolveReview(item.id, {
        mediaType: item.mediaType,
        tvdbId: state.idType === 'tvdb' ? idNum : undefined,
        tmdbId: state.idType === 'tmdb' ? idNum : undefined,
        seasons: state.seasons.trim() || '1',
        titleOverride: state.titleOverride.trim(),
      });
      successMessage = `Successfully resolved "${item.titleRomaji}" and created mapping override.`;
      await loadReviewQueue();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to resolve review item';
    } finally {
      processingId = null;
    }
  }

  async function handleIgnore(item: ReviewQueueItem) {
    const state = formStates[item.id];
    processingId = item.id;
    errorMessage = '';
    successMessage = '';

    try {
      await api.ignoreReview(item.id, state?.ignoreReason || 'Ignored from review queue');
      successMessage = `Routed "${item.titleRomaji}" to ignored titles.`;
      await loadReviewQueue();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to ignore review item';
    } finally {
      processingId = null;
    }
  }

  $: filteredItems = items.filter((item) => {
    if (!searchQuery.trim()) return true;
    const q = searchQuery.toLowerCase();
    return (
      item.titleRomaji.toLowerCase().includes(q) ||
      (item.titleEnglish && item.titleEnglish.toLowerCase().includes(q)) ||
      String(item.anilistId).includes(q) ||
      item.reason.toLowerCase().includes(q)
    );
  });

  function handleImageError(event: Event) {
    const target = event.currentTarget as HTMLElement | null;
    if (target) {
      target.style.display = 'none';
    }
  }

  onMount(loadReviewQueue);
</script>

<div class="space-y-6">
  <!-- Top header -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-[#24374e]">
    <div>
      <h2 class="text-xl font-bold text-white tracking-tight">Visual Review Queue</h2>
      <p class="text-xs text-slate-400 mt-0.5">
        Resolve unmapped anime titles with AniList posters, candidate search results, and override assignment.
      </p>
    </div>

    <div class="flex items-center gap-2">
      <div class="w-full sm:w-64">
        <input
          type="text"
          bind:value={searchQuery}
          placeholder="Search unmapped titles..."
          class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#152232] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
        />
      </div>

      <button
        type="button"
        on:click={loadReviewQueue}
        class="p-1.5 rounded-lg border border-[#24374e] text-slate-400 hover:text-white hover:bg-[#19273a] transition"
        title="Refresh queue"
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

  <!-- Queue Cards List -->
  {#if loading}
    <div class="p-12 text-center text-slate-400">
      <svg class="w-6 h-6 animate-spin mx-auto text-sky-400 mb-2" viewBox="0 0 24 24" fill="none">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
      </svg>
      <span class="text-xs">Loading review queue items...</span>
    </div>
  {:else if filteredItems.length === 0}
    <div class="p-12 text-center rounded-xl bg-[#152232] border border-[#24374e] text-slate-400 space-y-2">
      <div class="text-emerald-400 font-semibold text-base">Review Queue is Clear</div>
      <p class="text-xs text-slate-500 max-w-md mx-auto">
        All AniList anime entries have been mapped to Servarr or ignored. New items requiring review will appear here during future sync passes.
      </p>
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-4">
      {#each filteredItems as item (item.id)}
        {@const candidates = parseCandidates(item.candidatesJson)}
        {@const isBusy = processingId === item.id}
        <div class="rounded-xl bg-[#152232] border border-[#24374e] overflow-hidden shadow-lg p-5 flex flex-col md:flex-row gap-5">
          <!-- Poster Thumbnail -->
          <div class="w-full md:w-36 flex-shrink-0">
            <div class="w-full h-48 md:h-52 rounded-lg bg-[#0b1622] border border-[#24374e] overflow-hidden relative flex items-center justify-center">
              {#if item.posterUrl}
                  <img
                    src={item.posterUrl}
                    alt={item.titleRomaji}
                    class="w-full h-full object-cover"
                    on:error={handleImageError}
                  />
              {:else}
                <div class="text-center p-3 text-slate-500 text-xs">
                  <svg class="w-8 h-8 mx-auto mb-1 opacity-40" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <rect x="3" y="3" width="18" height="18" rx="2" stroke-width="2" />
                    <circle cx="8.5" cy="8.5" r="1.5" stroke-width="2" />
                    <path d="M21 15l-5-5L5 21" stroke-width="2" />
                  </svg>
                  <span>No Poster</span>
                </div>
              {/if}
            </div>
          </div>

          <!-- Metadata & Resolution Area -->
          <div class="flex-1 space-y-3">
            <!-- Title and Badges -->
            <div>
              <div class="flex flex-wrap items-center gap-2 mb-1">
                <span class="px-2 py-0.5 rounded text-[10px] font-bold bg-sky-950 border border-sky-800 text-sky-300">
                  {item.mediaType}
                </span>
                {#if item.releaseYear}
                  <span class="px-2 py-0.5 rounded text-[10px] font-medium bg-[#0b1622] border border-[#24374e] text-slate-300">
                    {item.releaseYear}
                  </span>
                {/if}
                <a
                  href={`https://anilist.co/anime/${item.anilistId}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-[11px] font-mono text-sky-400 hover:underline"
                >
                  AniList #{item.anilistId} ↗
                </a>
                <span class="px-2 py-0.5 rounded text-[10px] font-semibold bg-amber-950/70 border border-amber-800 text-amber-300">
                  Reason: {item.reason}
                </span>
              </div>

              <h3 class="text-base font-bold text-white tracking-tight">{item.titleRomaji}</h3>
              {#if item.titleEnglish && item.titleEnglish !== item.titleRomaji}
                <div class="text-xs text-slate-400 font-medium">{item.titleEnglish}</div>
              {/if}
            </div>

            <!-- Candidates (if any returned by resolver) -->
            {#if candidates.length > 0}
              <div class="p-3 rounded-lg bg-[#0b1622] border border-[#24374e] space-y-1.5">
                <div class="text-[11px] font-semibold text-slate-400">Suggested Candidates (Click to select):</div>
                <div class="flex flex-wrap gap-2">
                  {#each candidates as cand}
                    <button
                      type="button"
                      on:click={() => applyCandidate(item.id, cand)}
                      class="px-2.5 py-1 text-xs rounded bg-[#19273a] hover:bg-[#24374e] border border-[#24374e] hover:border-sky-500 text-slate-200 transition text-left flex items-center gap-2"
                    >
                      <span class="font-medium">{cand.title || 'Unknown'}</span>
                      {#if cand.tvdbId}
                        <span class="text-[10px] text-sky-400 font-mono">TVDB: {cand.tvdbId}</span>
                      {:else if cand.tmdbId}
                        <span class="text-[10px] text-amber-400 font-mono">TMDb: {cand.tmdbId}</span>
                      {/if}
                      {#if cand.score}
                        <span class="text-[10px] text-emerald-400 font-mono">({Math.round(cand.score * 100)}%)</span>
                      {/if}
                    </button>
                  {/each}
                </div>
              </div>
            {/if}

            <!-- Resolution Form -->
            {#if formStates[item.id]}
              <div class="pt-2 border-t border-[#24374e] grid grid-cols-1 sm:grid-cols-12 gap-3 items-end">
                <!-- ID Type Selector & Value -->
                <div class="sm:col-span-4 space-y-1">
                  <div class="flex items-center justify-between text-xs font-medium text-slate-300">
                    <div class="flex items-center gap-2">
                      <label class="cursor-pointer inline-flex items-center gap-1">
                        <input type="radio" bind:group={formStates[item.id].idType} value="tvdb" class="text-sky-500" />
                        <span>TVDB</span>
                      </label>
                      <label class="cursor-pointer inline-flex items-center gap-1">
                        <input type="radio" bind:group={formStates[item.id].idType} value="tmdb" class="text-sky-500" />
                        <span>TMDb</span>
                      </label>
                    </div>
                  </div>
                  <input
                    type="number"
                    bind:value={formStates[item.id].resolvedId}
                    placeholder={`Enter ${formStates[item.id].idType.toUpperCase()} ID`}
                    class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
                  />
                </div>

                <!-- Seasons mapping -->
                {#if item.mediaType === 'SERIES'}
                  <div class="sm:col-span-2 space-y-1">
                    <label for={`season-${item.id}`} class="text-xs font-medium text-slate-300">Season(s)</label>
                    <input
                      id={`season-${item.id}`}
                      type="text"
                      bind:value={formStates[item.id].seasons}
                      placeholder="1 or 1,2"
                      class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
                    />
                  </div>
                {/if}

                <!-- Title Override -->
                <div class="{item.mediaType === 'SERIES' ? 'sm:col-span-4' : 'sm:col-span-6'} space-y-1">
                  <label for={`title-${item.id}`} class="text-xs font-medium text-slate-300">Title Override (Optional)</label>
                  <input
                    id={`title-${item.id}`}
                    type="text"
                    bind:value={formStates[item.id].titleOverride}
                    placeholder="Custom series title"
                    class="w-full px-2.5 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
                  />
                </div>

                <!-- Resolve Button -->
                <div class="sm:col-span-2">
                  <button
                    type="button"
                    on:click={() => handleResolve(item)}
                    disabled={isBusy || !formStates[item.id].resolvedId}
                    class="w-full px-3 py-1.5 text-xs font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 text-slate-950 transition disabled:opacity-50 disabled:cursor-not-allowed shadow-sm"
                  >
                    {isBusy ? 'Saving...' : 'Resolve'}
                  </button>
                </div>
              </div>

              <!-- Ignore Control Accordion -->
              <div class="pt-1">
                {#if !formStates[item.id].showIgnoreForm}
                  <button
                    type="button"
                    on:click={() => {
                      formStates[item.id].showIgnoreForm = true;
                      formStates = { ...formStates };
                    }}
                    class="text-[11px] text-slate-400 hover:text-slate-300 underline"
                  >
                    Ignore this title instead
                  </button>
                {:else}
                  <div class="p-3 rounded-lg bg-[#0b1622] border border-[#24374e] flex flex-col sm:flex-row items-center gap-2">
                    <input
                      type="text"
                      bind:value={formStates[item.id].ignoreReason}
                      placeholder="Reason to ignore (e.g. Not anime, unavailable)"
                      class="flex-1 px-2.5 py-1.5 text-xs rounded-lg bg-[#152232] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
                    />
                    <button
                      type="button"
                      on:click={() => handleIgnore(item)}
                      disabled={isBusy}
                      class="px-3 py-1.5 text-xs font-medium rounded-lg bg-rose-500/20 text-rose-300 border border-rose-500/40 hover:bg-rose-500 hover:text-white transition disabled:opacity-50 whitespace-nowrap"
                    >
                      Confirm Ignore
                    </button>
                    <button
                      type="button"
                      on:click={() => {
                        formStates[item.id].showIgnoreForm = false;
                        formStates = { ...formStates };
                      }}
                      class="px-2 py-1.5 text-xs text-slate-400 hover:text-slate-200"
                    >
                      Cancel
                    </button>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
