<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api, type SyncStatus, type SyncHistoryRecord } from '../lib/api';

  export let isSyncing = false;

  let status: SyncStatus | null = null;
  let history: SyncHistoryRecord[] = [];
  let loading = true;
  let triggering = false;
  let errorMessage = '';
  let pollInterval: any = null;

  async function loadData() {
    try {
      errorMessage = '';
      const [statusRes, historyRes] = await Promise.all([
        api.getSyncStatus(),
        api.getSyncHistory(),
      ]);
      status = statusRes;
      history = historyRes;
      isSyncing = statusRes.running;
    } catch (err: any) {
      errorMessage = err.message || 'Failed to load sync data';
    } finally {
      loading = false;
    }
  }

  async function handleTriggerSync() {
    triggering = true;
    errorMessage = '';
    try {
      await api.triggerSync();
      isSyncing = true;
      await loadData();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to trigger sync pass';
    } finally {
      triggering = false;
    }
  }

  function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    const sec = (ms / 1000).toFixed(1);
    return `${sec}s`;
  }

  function formatTime(iso: string): string {
    if (!iso) return '-';
    const date = new Date(iso);
    return date.toLocaleString();
  }

  onMount(() => {
    loadData();
    pollInterval = setInterval(async () => {
      try {
        const statusRes = await api.getSyncStatus();
        status = statusRes;
        isSyncing = statusRes.running;
        if (!statusRes.running) {
          const historyRes = await api.getSyncHistory();
          history = historyRes;
        }
      } catch {
        // Silently continue polling
      }
    }, 3000);
  });

  onDestroy(() => {
    if (pollInterval) clearInterval(pollInterval);
  });
</script>

<div class="space-y-6">
  <!-- Top header with Trigger button -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-[#24374e]">
    <div>
      <h2 class="text-xl font-bold text-white tracking-tight">Sync Operations</h2>
      <p class="text-xs text-slate-400 mt-0.5">
        Live synchronization metrics, dry-run orchestrator status, and manual triggers.
      </p>
    </div>

    <button
      type="button"
      on:click={handleTriggerSync}
      disabled={isSyncing || triggering}
      class="inline-flex items-center justify-center gap-2 px-4 py-2 text-sm font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 active:bg-sky-600 text-slate-950 transition disabled:opacity-50 disabled:cursor-not-allowed shadow-lg shadow-sky-500/20"
    >
      {#if isSyncing || triggering}
        <svg class="w-4 h-4 animate-spin text-slate-950" viewBox="0 0 24 24" fill="none">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
        </svg>
        <span>Syncing in Progress...</span>
      {:else}
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
        <span>Trigger Sync Now</span>
      {/if}
    </button>
  </div>

  {#if errorMessage}
    <div class="p-3 rounded-lg bg-rose-950/60 border border-rose-800 text-rose-300 text-sm flex items-center justify-between">
      <span>{errorMessage}</span>
      <button class="text-xs font-semibold hover:underline" on:click={loadData}>Retry</button>
    </div>
  {/if}

  <!-- Metrics Grid -->
  {#if status && status.lastReport}
    {@const r = status.lastReport}
    <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
      <div class="p-4 rounded-xl bg-[#152232] border border-[#24374e]">
        <div class="text-xs font-medium text-slate-400">Scanned Items</div>
        <div class="text-2xl font-bold text-white mt-1">{r.itemsScanned}</div>
        <div class="text-[11px] text-slate-500 mt-1">AniList library</div>
      </div>

      <div class="p-4 rounded-xl bg-[#152232] border border-[#24374e]">
        <div class="text-xs font-medium text-slate-400">Added to Radarr</div>
        <div class="text-2xl font-bold text-emerald-400 mt-1">{r.addedRadarr}</div>
        <div class="text-[11px] text-slate-500 mt-1">Anime movies</div>
      </div>

      <div class="p-4 rounded-xl bg-[#152232] border border-[#24374e]">
        <div class="text-xs font-medium text-slate-400">Monitored Sonarr</div>
        <div class="text-2xl font-bold text-sky-400 mt-1">{r.monitoredSonarr}</div>
        <div class="text-[11px] text-slate-500 mt-1">Series & seasons</div>
      </div>

      <div class="p-4 rounded-xl bg-[#152232] border border-[#24374e]">
        <div class="text-xs font-medium text-slate-400">Unmonitored</div>
        <div class="text-2xl font-bold text-slate-300 mt-1">{r.unmonitoredSonarr + r.unmonitoredRadarr}</div>
        <div class="text-[11px] text-slate-500 mt-1">{r.unmonitoredSonarr} TV, {r.unmonitoredRadarr} Movies</div>
      </div>

      <div class="p-4 rounded-xl bg-[#152232] border border-[#24374e]">
        <div class="text-xs font-medium text-slate-400">Review Queue</div>
        <div class="text-2xl font-bold text-amber-400 mt-1">{r.queuedReview}</div>
        <div class="text-[11px] text-slate-500 mt-1">Needs attention</div>
      </div>

      <div class="p-4 rounded-xl bg-[#152232] border border-[#24374e]">
        <div class="text-xs font-medium text-slate-400">Last Duration</div>
        <div class="text-2xl font-bold text-white mt-1">{formatDuration(r.durationMs)}</div>
        <div class="text-[11px] text-slate-500 mt-1">{r.trigger}</div>
      </div>
    </div>
  {:else if !loading}
    <div class="p-6 rounded-xl bg-[#152232] border border-[#24374e] text-center text-slate-400">
      <p class="text-sm">No recent sync report available. Click "Trigger Sync Now" to run an initial synchronization.</p>
    </div>
  {/if}

  <!-- Recent Execution History Table -->
  <div class="space-y-3">
    <div class="flex items-center justify-between">
      <h3 class="text-base font-semibold text-white">Execution History</h3>
      <span class="text-xs text-slate-400">{history.length} logged runs</span>
    </div>

    {#if history.length === 0}
      <div class="p-8 text-center rounded-xl bg-[#152232] border border-[#24374e] text-slate-400 text-sm">
        No past synchronization runs recorded in the database yet.
      </div>
    {:else}
      <div class="overflow-x-auto rounded-xl border border-[#24374e] bg-[#152232]">
        <table class="w-full text-left text-xs text-slate-300 divide-y divide-[#24374e]">
          <thead class="bg-[#19273a] text-slate-400 uppercase font-semibold tracking-wider text-[11px]">
            <tr>
              <th class="px-4 py-3">ID</th>
              <th class="px-4 py-3">Timestamp</th>
              <th class="px-4 py-3">Trigger</th>
              <th class="px-4 py-3">Status</th>
              <th class="px-4 py-3">Duration</th>
              <th class="px-4 py-3">Scanned</th>
              <th class="px-4 py-3">Radarr Added</th>
              <th class="px-4 py-3">Sonarr Monitored</th>
              <th class="px-4 py-3">Review Queued</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#24374e]/60">
            {#each history as item (item.id)}
              <tr class="hover:bg-[#19273a]/60 transition">
                <td class="px-4 py-3 font-mono text-slate-400">{item.id}</td>
                <td class="px-4 py-3 whitespace-nowrap">{formatTime(item.runTimestamp)}</td>
                <td class="px-4 py-3 font-medium text-slate-200">{item.triggerType}</td>
                <td class="px-4 py-3">
                  <span
                    class="px-2 py-0.5 rounded-full text-[10px] font-bold uppercase {item.status === 'SUCCESS' ? 'bg-emerald-950 border border-emerald-700 text-emerald-300' : item.status === 'PARTIAL' ? 'bg-amber-950 border border-amber-700 text-amber-300' : 'bg-rose-950 border border-rose-700 text-rose-300'}"
                  >
                    {item.status}
                  </span>
                </td>
                <td class="px-4 py-3 font-mono">{formatDuration(item.durationMs)}</td>
                <td class="px-4 py-3 font-mono">{item.itemsScanned}</td>
                <td class="px-4 py-3 font-mono text-emerald-400">{item.addedRadarr}</td>
                <td class="px-4 py-3 font-mono text-sky-400">{item.monitoredSonarr}</td>
                <td class="px-4 py-3 font-mono {item.queuedReview > 0 ? 'text-amber-400' : 'text-slate-400'}">
                  {item.queuedReview}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
</div>
