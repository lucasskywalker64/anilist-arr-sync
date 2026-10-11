<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { getApiKey, setApiKey, api } from '../lib/api';

  export let open = false;

  const dispatch = createEventDispatcher();

  let keyInput = getApiKey();
  let statusMessage = '';
  let statusError = false;
  let testing = false;

  function handleSave() {
    setApiKey(keyInput.trim());
    statusMessage = 'API key saved locally.';
    statusError = false;
    dispatch('saved');
    setTimeout(() => {
      open = false;
      statusMessage = '';
    }, 800);
  }

  async function handleTest() {
    testing = true;
    statusMessage = '';
    statusError = false;
    try {
      setApiKey(keyInput.trim());
      const res = await api.getHealth();
      statusMessage = `Connection successful! Version: ${res.version}`;
      statusError = false;
    } catch (err: any) {
      statusError = true;
      statusMessage = `Connection failed: ${err.message || 'Unknown error'}`;
    } finally {
      testing = false;
    }
  }

  function handleClose() {
    open = false;
  }
</script>

{#if open}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm">
    <div class="w-full max-w-md rounded-xl bg-[#152232] border border-[#24374e] p-6 shadow-2xl space-y-4">
      <div class="flex items-center justify-between pb-2 border-b border-[#24374e]">
        <h3 class="text-lg font-semibold text-slate-100">API Key & Authentication</h3>
        <button
          class="text-slate-400 hover:text-slate-200 text-xl font-bold p-1 leading-none"
          on:click={handleClose}
          aria-label="Close"
        >
          &times;
        </button>
      </div>

      <p class="text-xs text-slate-400 leading-relaxed">
        State-mutating operations like triggering syncs, resolving review items, and editing overrides require the configured <code class="text-sky-400 font-mono">X-Api-Key</code> header.
      </p>

      <div class="space-y-1.5">
        <label for="apikey-input" class="text-xs font-medium text-slate-300">Server API Key</label>
        <input
          id="apikey-input"
          type="password"
          bind:value={keyInput}
          placeholder="Paste X-Api-Key here"
          class="w-full px-3 py-2 text-sm rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500 focus:ring-1 focus:ring-sky-500 font-mono"
        />
      </div>

      {#if statusMessage}
        <div
          class="p-2.5 rounded-lg text-xs {statusError ? 'bg-rose-950/60 border border-rose-800 text-rose-300' : 'bg-emerald-950/60 border border-emerald-800 text-emerald-300'}"
        >
          {statusMessage}
        </div>
      {/if}

      <div class="flex items-center justify-end gap-2 pt-2">
        <button
          type="button"
          on:click={handleTest}
          disabled={testing}
          class="px-3 py-1.5 text-xs font-medium rounded-lg border border-[#24374e] text-slate-300 hover:bg-[#19273a] disabled:opacity-50 transition"
        >
          {testing ? 'Testing...' : 'Test Connection'}
        </button>
        <button
          type="button"
          on:click={handleSave}
          class="px-4 py-1.5 text-xs font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 text-slate-950 transition"
        >
          Save Key
        </button>
      </div>
    </div>
  </div>
{/if}
