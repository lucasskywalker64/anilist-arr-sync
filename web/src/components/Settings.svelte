<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AppConfig } from '../lib/api';

  let config: AppConfig = {};
  let loading = true;
  let saving = false;
  let errorMessage = '';
  let successMessage = '';

  async function loadConfig() {
    loading = true;
    errorMessage = '';
    try {
      config = await api.getConfig();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to load configuration';
    } finally {
      loading = false;
    }
  }

  async function handleSave() {
    saving = true;
    errorMessage = '';
    successMessage = '';

    try {
      await api.updateConfig({
        instanceName: config.instanceName,
        aniListUsername: config.aniListUsername,
        radarrUrl: config.radarrUrl,
        radarrApiKey: config.radarrApiKey,
        sonarrUrl: config.sonarrUrl,
        sonarrApiKey: config.sonarrApiKey,
        tagName: config.tagName,
        syncIntervalHours: config.syncIntervalHours,
        dateToleranceDays: config.dateToleranceDays,
        confidenceThreshold: config.confidenceThreshold,
        unmonitorDropped: config.unmonitorDropped,
        firstRunDryRun: config.firstRunDryRun,
      });
      successMessage = 'Configuration updated and persisted successfully.';
      await loadConfig();
    } catch (err: any) {
      errorMessage = err.message || 'Failed to save configuration';
    } finally {
      saving = false;
    }
  }

  onMount(loadConfig);
</script>

<div class="space-y-6 max-w-4xl">
  <!-- Top header -->
  <div class="pb-4 border-b border-[#24374e]">
    <h2 class="text-xl font-bold text-white tracking-tight">System Settings</h2>
    <p class="text-xs text-slate-400 mt-0.5">
      Configure AniList credentials, Sonarr/Radarr integrations, and sync engine heuristics.
    </p>
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

  {#if loading}
    <div class="p-12 text-center text-slate-400">
      <svg class="w-6 h-6 animate-spin mx-auto text-sky-400 mb-2" viewBox="0 0 24 24" fill="none">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
      </svg>
      <span class="text-xs">Loading settings...</span>
    </div>
  {:else}
    <form on:submit|preventDefault={handleSave} class="space-y-6">
      <!-- General section -->
      <div class="p-5 rounded-xl bg-[#152232] border border-[#24374e] space-y-4">
        <h3 class="text-sm font-semibold text-white border-b border-[#24374e] pb-2">General & AniList</h3>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="space-y-1">
            <label for="cfg-instance-name" class="text-xs font-medium text-slate-300">Instance Name</label>
            <input
              id="cfg-instance-name"
              type="text"
              bind:value={config.instanceName}
              placeholder="AniList Sync Instance"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
            />
          </div>

          <div class="space-y-1">
            <label for="cfg-anilist-username" class="text-xs font-medium text-slate-300">AniList Username *</label>
            <input
              id="cfg-anilist-username"
              type="text"
              bind:value={config.aniListUsername}
              placeholder="Your AniList username"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
            />
          </div>

          <div class="space-y-1">
            <label for="cfg-tag-name" class="text-xs font-medium text-slate-300">Servarr Tag Name</label>
            <input
              id="cfg-tag-name"
              type="text"
              bind:value={config.tagName}
              placeholder="anilist"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
            />
          </div>
        </div>
      </div>

      <!-- Sonarr integration -->
      <div class="p-5 rounded-xl bg-[#152232] border border-[#24374e] space-y-4">
        <h3 class="text-sm font-semibold text-white border-b border-[#24374e] pb-2">Sonarr Integration</h3>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="space-y-1">
            <label for="cfg-sonarr-url" class="text-xs font-medium text-slate-300">Sonarr URL</label>
            <input
              id="cfg-sonarr-url"
              type="text"
              bind:value={config.sonarrUrl}
              placeholder="http://localhost:8989"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>

          <div class="space-y-1">
            <label for="cfg-sonarr-apikey" class="text-xs font-medium text-slate-300">Sonarr API Key</label>
            <input
              id="cfg-sonarr-apikey"
              type="password"
              bind:value={config.sonarrApiKey}
              placeholder="Leave blank or ****** to keep unchanged"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>
        </div>
      </div>

      <!-- Radarr integration -->
      <div class="p-5 rounded-xl bg-[#152232] border border-[#24374e] space-y-4">
        <h3 class="text-sm font-semibold text-white border-b border-[#24374e] pb-2">Radarr Integration</h3>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="space-y-1">
            <label for="cfg-radarr-url" class="text-xs font-medium text-slate-300">Radarr URL</label>
            <input
              id="cfg-radarr-url"
              type="text"
              bind:value={config.radarrUrl}
              placeholder="http://localhost:7878"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>

          <div class="space-y-1">
            <label for="cfg-radarr-apikey" class="text-xs font-medium text-slate-300">Radarr API Key</label>
            <input
              id="cfg-radarr-apikey"
              type="password"
              bind:value={config.radarrApiKey}
              placeholder="Leave blank or ****** to keep unchanged"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>
        </div>
      </div>

      <!-- Sync heuristics & scheduler -->
      <div class="p-5 rounded-xl bg-[#152232] border border-[#24374e] space-y-4">
        <h3 class="text-sm font-semibold text-white border-b border-[#24374e] pb-2">Sync Engine Heuristics</h3>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div class="space-y-1">
            <label for="cfg-sync-interval" class="text-xs font-medium text-slate-300">Sync Interval (Hours)</label>
            <input
              id="cfg-sync-interval"
              type="number"
              bind:value={config.syncIntervalHours}
              min="1"
              max="168"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>

          <div class="space-y-1">
            <label for="cfg-date-tolerance" class="text-xs font-medium text-slate-300">Date Tolerance (Days)</label>
            <input
              id="cfg-date-tolerance"
              type="number"
              bind:value={config.dateToleranceDays}
              min="0"
              max="365"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>

          <div class="space-y-1">
            <label for="cfg-confidence-threshold" class="text-xs font-medium text-slate-300">Confidence Threshold</label>
            <input
              id="cfg-confidence-threshold"
              type="number"
              bind:value={config.confidenceThreshold}
              step="0.05"
              min="0.1"
              max="1.0"
              class="w-full px-3 py-1.5 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 focus:outline-none focus:border-sky-500 font-mono"
            />
          </div>
        </div>

        <div class="pt-3 border-t border-[#24374e] flex flex-col sm:flex-row gap-6">
          <label class="flex items-center gap-2.5 cursor-pointer text-xs text-slate-300">
            <input
              type="checkbox"
              bind:checked={config.unmonitorDropped}
              class="rounded border-[#24374e] bg-[#0b1622] text-sky-500 focus:ring-0"
            />
            <span>Unmonitor series dropped on AniList</span>
          </label>

          <label class="flex items-center gap-2.5 cursor-pointer text-xs text-slate-300">
            <input
              type="checkbox"
              bind:checked={config.firstRunDryRun}
              class="rounded border-[#24374e] bg-[#0b1622] text-sky-500 focus:ring-0"
            />
            <span>Enable First Run Dry-Run (stage actions first)</span>
          </label>
        </div>
      </div>

      <!-- Save button -->
      <div class="flex justify-end pt-2">
        <button
          type="submit"
          disabled={saving}
          class="px-5 py-2 text-sm font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 text-slate-950 transition disabled:opacity-50 shadow-lg shadow-sky-500/20"
        >
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </form>
  {/if}
</div>
