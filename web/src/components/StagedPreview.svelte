<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { api, type StagedAction } from '../lib/api';

  const dispatch = createEventDispatcher();

  export let stagedCount = 0;

  let items: StagedAction[] = [];
  let loading = true;
  let processing = false;
  let errorMessage = '';
  let successMessage = '';
  let searchQuery = '';
  let filterType = 'ALL';

  let selectedIds = new Set<number>();

  async function loadStaged() {
    loading = true;
    errorMessage = '';
    try {
      items = await api.getStagedActions();
      stagedCount = items.filter((i) => i.status === 'PENDING').length;
      dispatch('countChange', stagedCount);
      // Clean up selectedIds if items no longer present
      const validIds = new Set(items.map((i) => i.id));
      selectedIds = new Set([...selectedIds].filter((id) => validIds.has(id)));
    } catch (err: any) {
      errorMessage = err.message || 'Failed to load staged sync actions';
    } finally {
      loading = false;
    }
  }

  function toggleSelect(id: number) {
    if (selectedIds.has(id)) {
      selectedIds.delete(id);
    } else {
      selectedIds.add(id);
    }
    selectedIds = new Set(selectedIds);
  }

  function toggleSelectAll() {
    const pending = filteredItems.filter((i) => i.status === 'PENDING');
    if (selectedIds.size === pending.length && pending.length > 0) {
      selectedIds.clear();
    } else {
      selectedIds = new Set(pending.map((i) => i.id));
    }
    selectedIds = new Set(selectedIds);
  }

  async function handleApply(ids?: number[]) {
    processing = true;
    errorMessage = '';
    successMessage = '';
    try {
      const res = await api.applyStaged(ids);
      successMessage = `Successfully applied ${res.appliedCount} action(s).`;
      selectedIds.clear();
      selectedIds = new Set();
      await loadStaged();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to apply staged action(s)';
    } finally {
      processing = false;
    }
  }

  async function handleReject(ids?: number[]) {
    processing = true;
    errorMessage = '';
    successMessage = '';
    try {
      const res = await api.rejectStaged(ids);
      successMessage = `Successfully rejected ${res.rejectedCount} action(s).`;
      selectedIds.clear();
      selectedIds = new Set();
      await loadStaged();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to reject staged action(s)';
    } finally {
      processing = false;
    }
  }

  function parsePayload(payloadJson: string): any {
    try {
      return JSON.parse(payloadJson);
    } catch {
      return {};
    }
  }

  function formatEpisodes(episodes?: number[]): string {
    if (!episodes || episodes.length === 0) return '';
    if (episodes.length <= 4) {
      return `Episodes: [${episodes.join(', ')}]`;
    }
    return `Episodes: [${episodes[0]}–${episodes[episodes.length - 1]}] (${episodes.length} episodes)`;
  }

  $: filteredItems = items.filter((item) => {
    if (filterType !== 'ALL' && item.actionType !== filterType) {
      return false;
    }
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      return (
        item.title.toLowerCase().includes(q) ||
        item.targetService.toLowerCase().includes(q) ||
        item.actionType.toLowerCase().includes(q)
      );
    }
    return true;
  });

  $: allPendingSelected =
    filteredItems.filter((i) => i.status === 'PENDING').length > 0 &&
    filteredItems.filter((i) => i.status === 'PENDING').every((i) => selectedIds.has(i.id));

  onMount(loadStaged);
</script>

<div class="space-y-6">
  <!-- Top bar with Filter & Bulk action buttons -->
  <div class="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-[#24374e]">
    <div>
      <h2 class="text-xl font-bold text-white tracking-tight">Staged Sync Actions</h2>
      <p class="text-xs text-slate-400 mt-0.5">
        Interactive preview table for staged dry-run operations, split cour slice monitoring, and bulk approvals.
      </p>
    </div>

    <!-- Bulk actions -->
    <div class="flex flex-wrap items-center gap-2">
      {#if selectedIds.size > 0}
        <button
          type="button"
          on:click={() => handleApply([...selectedIds])}
          disabled={processing}
          class="px-3 py-1.5 text-xs font-semibold rounded-lg bg-emerald-500 hover:bg-emerald-400 text-slate-950 transition disabled:opacity-50 shadow-sm"
        >
          Approve Selected ({selectedIds.size})
        </button>
        <button
          type="button"
          on:click={() => handleReject([...selectedIds])}
          disabled={processing}
          class="px-3 py-1.5 text-xs font-semibold rounded-lg bg-rose-500/20 hover:bg-rose-500/30 text-rose-300 border border-rose-500/40 transition disabled:opacity-50"
        >
          Reject Selected ({selectedIds.size})
        </button>
      {/if}

      {#if items.some((i) => i.status === 'PENDING')}
        <button
          type="button"
          on:click={() => handleApply()}
          disabled={processing}
          class="px-3 py-1.5 text-xs font-medium rounded-lg border border-emerald-500/40 text-emerald-300 hover:bg-emerald-500/10 transition disabled:opacity-50"
        >
          Approve All
        </button>
        <button
          type="button"
          on:click={() => handleReject()}
          disabled={processing}
          class="px-3 py-1.5 text-xs font-medium rounded-lg border border-[#24374e] text-slate-400 hover:text-slate-200 hover:bg-[#19273a] transition disabled:opacity-50"
        >
          Reject All
        </button>
      {/if}

      <button
        type="button"
        on:click={loadStaged}
        class="p-1.5 rounded-lg border border-[#24374e] text-slate-400 hover:text-white hover:bg-[#19273a] transition"
        title="Refresh staged actions"
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

  <!-- Filter & Search Controls -->
  <div class="flex flex-col sm:flex-row gap-3 items-center justify-between">
    <div class="w-full sm:w-72">
      <input
        type="text"
        bind:value={searchQuery}
        placeholder="Filter by title or service..."
        class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#152232] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
      />
    </div>

    <div class="flex items-center gap-2 self-start sm:self-auto text-xs">
      <span class="text-slate-400">Action:</span>
      <select
        bind:value={filterType}
        class="px-2.5 py-1.5 rounded-lg bg-[#152232] border border-[#24374e] text-slate-200 text-xs focus:outline-none focus:border-sky-500"
      >
        <option value="ALL">All Actions</option>
        <option value="ADD_SERIES">Add Series</option>
        <option value="ADD_MOVIE">Add Movie</option>
        <option value="MONITOR_SEASON">Monitor Season / Split Cour</option>
        <option value="UNMONITOR_SEASON">Unmonitor Season</option>
        <option value="UNMONITOR_MOVIE">Unmonitor Movie</option>
      </select>
    </div>
  </div>

  <!-- Interactive Table -->
  {#if loading}
    <div class="p-12 text-center text-slate-400">
      <svg class="w-6 h-6 animate-spin mx-auto text-sky-400 mb-2" viewBox="0 0 24 24" fill="none">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
      </svg>
      <span class="text-xs">Loading staged operations...</span>
    </div>
  {:else if filteredItems.length === 0}
    <div class="p-12 text-center rounded-xl bg-[#152232] border border-[#24374e] text-slate-400 space-y-2">
      <div class="text-slate-300 font-medium text-sm">No staged actions pending</div>
      <p class="text-xs text-slate-500 max-w-md mx-auto">
        When the orchestrator operates in dry-run mode or identifies pending operations, planned changes appear here for review.
      </p>
    </div>
  {:else}
    <div class="overflow-x-auto rounded-xl border border-[#24374e] bg-[#152232]">
      <table class="w-full text-left text-xs text-slate-300 divide-y divide-[#24374e]">
        <thead class="bg-[#19273a] text-slate-400 uppercase font-semibold tracking-wider text-[11px]">
          <tr>
            <th class="px-4 py-3 w-10">
              <input
                type="checkbox"
                checked={allPendingSelected}
                on:change={toggleSelectAll}
                class="rounded border-[#24374e] bg-[#0b1622] text-sky-500 focus:ring-0 focus:ring-offset-0 cursor-pointer"
                title="Select all pending"
              />
            </th>
            <th class="px-4 py-3">Action Type</th>
            <th class="px-4 py-3">Target</th>
            <th class="px-4 py-3">Title & Slice Details</th>
            <th class="px-4 py-3">Status</th>
            <th class="px-4 py-3">Created</th>
            <th class="px-4 py-3 text-right">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#24374e]/60">
          {#each filteredItems as item (item.id)}
            {@const payload = parsePayload(item.payloadJson)}
            {@const isPending = item.status === 'PENDING'}
            <tr class="hover:bg-[#19273a]/60 transition {selectedIds.has(item.id) ? 'bg-sky-500/5' : ''}">
              <td class="px-4 py-3">
                <input
                  type="checkbox"
                  disabled={!isPending}
                  checked={selectedIds.has(item.id)}
                  on:change={() => toggleSelect(item.id)}
                  class="rounded border-[#24374e] bg-[#0b1622] text-sky-500 focus:ring-0 focus:ring-offset-0 cursor-pointer disabled:opacity-30"
                />
              </td>

              <td class="px-4 py-3 whitespace-nowrap">
                {#if item.actionType === 'ADD_SERIES' || item.actionType === 'ADD_MOVIE'}
                  <span class="px-2 py-0.5 rounded text-[10px] font-bold bg-emerald-950 border border-emerald-700 text-emerald-300">
                    {item.actionType}
                  </span>
                {:else if item.actionType === 'MONITOR_SEASON'}
                  <span class="px-2 py-0.5 rounded text-[10px] font-bold bg-sky-950 border border-sky-700 text-sky-300">
                    MONITOR_SEASON
                  </span>
                {:else}
                  <span class="px-2 py-0.5 rounded text-[10px] font-bold bg-amber-950 border border-amber-700 text-amber-300">
                    {item.actionType}
                  </span>
                {/if}
              </td>

              <td class="px-4 py-3 whitespace-nowrap font-medium text-slate-200">
                <span class="inline-flex items-center gap-1">
                  {#if item.targetService === 'Sonarr'}
                    <span class="w-2 h-2 rounded-full bg-sky-400"></span>
                  {:else}
                    <span class="w-2 h-2 rounded-full bg-amber-400"></span>
                  {/if}
                  {item.targetService}
                </span>
              </td>

              <td class="px-4 py-3">
                <div class="font-medium text-white">{item.title}</div>
                <!-- Slice and split cour details -->
                {#if item.actionType === 'MONITOR_SEASON' || item.actionType === 'UNMONITOR_SEASON'}
                  <div class="text-[11px] text-slate-400 mt-0.5 flex flex-wrap items-center gap-1.5">
                    <span class="px-1.5 py-0.2 rounded bg-[#0b1622] border border-[#24374e] text-slate-300">
                      Season {payload.seasonNumber ?? 1}
                    </span>
                    {#if payload.episodeIds && payload.episodeIds.length > 0}
                      <span class="px-1.5 py-0.2 rounded bg-sky-950 border border-sky-800 text-sky-300 font-mono">
                        {formatEpisodes(payload.episodeIds)}
                      </span>
                      <span class="text-[10px] text-sky-400/80 font-medium">(Split Cour Slice)</span>
                    {:else}
                      <span class="text-slate-500">(Entire Season)</span>
                    {/if}
                  </div>
                {/if}
              </td>

              <td class="px-4 py-3 whitespace-nowrap">
                <span
                  class="px-2 py-0.5 rounded-full text-[10px] font-bold uppercase {item.status === 'PENDING' ? 'bg-amber-950 border border-amber-700 text-amber-300' : item.status === 'APPLIED' ? 'bg-emerald-950 border border-emerald-700 text-emerald-300' : 'bg-slate-800 text-slate-400'}"
                >
                  {item.status}
                </span>
              </td>

              <td class="px-4 py-3 whitespace-nowrap text-slate-400 text-[11px]">
                {new Date(item.createdAt).toLocaleDateString()}
              </td>

              <td class="px-4 py-3 whitespace-nowrap text-right">
                {#if isPending}
                  <div class="inline-flex items-center gap-1.5">
                    <button
                      type="button"
                      on:click={() => handleApply([item.id])}
                      disabled={processing}
                      class="px-2.5 py-1 text-[11px] font-semibold rounded bg-emerald-500/20 text-emerald-300 hover:bg-emerald-500 hover:text-slate-950 border border-emerald-500/30 transition disabled:opacity-50"
                    >
                      Approve
                    </button>
                    <button
                      type="button"
                      on:click={() => handleReject([item.id])}
                      disabled={processing}
                      class="px-2.5 py-1 text-[11px] font-semibold rounded bg-rose-500/10 text-rose-300 hover:bg-rose-500 hover:text-white border border-rose-500/30 transition disabled:opacity-50"
                    >
                      Reject
                    </button>
                  </div>
                {:else}
                  <span class="text-slate-500 text-[11px] italic">Resolved</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
