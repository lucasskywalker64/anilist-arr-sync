<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { MoreVertical, RotateCw, Power, Keyboard } from 'lucide-svelte';
  import { api, waitForRestart } from '../../lib/api';
  import Modal from '../common/Modal.svelte';
  import Button from '../common/Button.svelte';

  let menu: HTMLDetailsElement;
  let isContainerized = true;
  let action: 'restart' | 'shutdown' | 'shortcuts' | '' = '';
  let busy = false;
  let reconnecting = false;
  let stopped = false;
  let reconnectUrl = '';
  let requiresNavigation = false;
  let error = '';
  const polling = new AbortController();

  onMount(() => {
    api.getSystemStatus().then(status => { isContainerized = status.isContainerized; }).catch(() => {});
  });
  onDestroy(() => polling.abort());

  function choose(next: typeof action) {
    menu.open = false;
    error = '';
    action = next;
  }

  async function confirm() {
    busy = true;
    error = '';
    try {
      if (action === 'restart') {
        const result = await api.restart();
        action = '';
        reconnecting = true;
        reconnectUrl = result.reconnectUrl;
        const target = new URL(result.reconnectUrl);
        const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(target.hostname);
        if (window.location.protocol === 'https:' && target.protocol === 'http:' && !result.statusUrl && !loopback) {
          requiresNavigation = true;
          error = 'Automatic reconnection is unavailable for this address.';
          return;
        }
        await waitForRestart(result.instanceId, result.reconnectUrl, polling.signal, result.statusUrl);
        if (target.origin === window.location.origin && target.pathname === `${window.AniListArrSync?.urlBase || ''}/`) {
          window.location.reload();
        } else {
          window.location.assign(result.reconnectUrl);
        }
      } else {
        await api.shutdown();
        action = '';
        stopped = true;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Action failed';
      if (reconnecting) requiresNavigation = true;
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window on:keydown={(e) => { if (e.key === 'Escape' && menu) menu.open = false; }} />

<details class="actions" bind:this={menu}>
  <summary title="Application actions" aria-label="Application actions"><MoreVertical size={20} /></summary>
  <div class="menu">
    <button on:click={() => choose('restart')}><RotateCw size={16} /> Restart</button>
    {#if !isContainerized}
      <button on:click={() => choose('shutdown')}><Power size={16} /> Shutdown</button>
    {/if}
    <button on:click={() => choose('shortcuts')}><Keyboard size={16} /> Keyboard Shortcuts</button>
  </div>
</details>

<Modal isOpen={action !== ''} title={action === 'shortcuts' ? 'Keyboard shortcuts' : action === 'restart' ? 'Restart application' : 'Shutdown application'} size="small" on:close={() => { if (!busy) action = ''; }}>
  {#if action === 'shortcuts'}
    <p><kbd>Tab</kbd> / <kbd>Shift+Tab</kbd>: Move between controls.</p>
    <p><kbd>Enter</kbd> / <kbd>Space</kbd>: Activate a focused control.</p>
    <p><kbd>Escape</kbd>: Close a dialog or the actions menu.</p>
  {:else}
    <p>{action === 'restart' ? 'Restart and reload configuration? Active work will be stopped. This page reconnects automatically.' : 'Shut down the application? You will need to start it again to reconnect.'}</p>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="buttons">
      <Button disabled={busy} on:click={() => action = ''}>Cancel</Button>
      <Button disabled={busy} on:click={confirm}>{busy ? 'Please wait...' : action === 'restart' ? 'Restart' : 'Shutdown'}</Button>
    </div>
  {/if}
</Modal>

{#if reconnecting || stopped}
  <div class="overlay" role="dialog" aria-modal="true" aria-label={stopped ? 'Application stopped' : 'Reconnecting'} tabindex="-1">
    <div class="message">
      {#if reconnecting && !requiresNavigation}<RotateCw size={28} class="spinning" />{/if}
      <h2>{stopped ? 'Application stopped' : requiresNavigation ? 'Reconnect to application' : 'Restarting application'}</h2>
      <p>{stopped ? 'Start the application to reconnect.' : requiresNavigation ? 'Open the application address to reconnect.' : 'Waiting for the application to reconnect...'}</p>
      {#if requiresNavigation}<a href={reconnectUrl}>Reconnect to application</a>{/if}
      {#if error}<p role="alert">{error}</p>{/if}
    </div>
  </div>
{/if}

<style>
  .actions { position: relative; }
  summary { display: flex; cursor: pointer; padding: 6px; border-radius: var(--borderRadius); }
  summary:hover, .menu button:hover { background: var(--pageBackground); }
  summary::-webkit-details-marker { display: none; }
  .menu { position: absolute; top: 100%; right: 0; min-width: 200px; padding: 6px; background: var(--pageHeaderBackgroundColor); border: 1px solid var(--borderColor); border-radius: var(--borderRadius); box-shadow: 0 4px 12px #0006; }
  .menu button { display: flex; align-items: center; gap: 10px; width: 100%; padding: 10px; color: var(--white); background: transparent; border: 0; text-align: left; cursor: pointer; }
  .buttons { display: flex; justify-content: flex-end; gap: 10px; margin-top: 20px; }
  .overlay { position: fixed; inset: 0; z-index: 1100; display: grid; place-items: center; background: #000c; color: var(--textColor); }
  .message { padding: 32px; text-align: center; background: var(--pageBackground); border-radius: var(--borderRadius); }
</style>
