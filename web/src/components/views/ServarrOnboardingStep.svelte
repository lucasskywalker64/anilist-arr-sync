<script lang="ts">
  import { api } from '../../lib/api';
  import type { ServarrConnectionOptions } from '../../lib/types';
  import Alert from '../common/Alert.svelte';
  import Button from '../common/Button.svelte';

  export let service: 'sonarr' | 'radarr';
  export let draft: {
    baseUrl: string;
    apiKey: string;
    savedUrl: string;
    hasSavedKey: boolean;
    profileId: number;
    rootFolder: string;
    options: ServarrConnectionOptions | null;
  };
  export let busy = false;

  let error = '';
  $: name = service === 'sonarr' ? 'Sonarr' : 'Radarr';
  $: useSavedKey = draft.hasSavedKey && draft.baseUrl.trim().replace(/\/+$/, '') === draft.savedUrl.trim().replace(/\/+$/, '');

  function invalidate() {
    draft.options = null;
    error = '';
  }

  async function testConnection() {
    invalidate();
    busy = true;
    try {
      const options = await api.testServarr(service, draft.baseUrl.trim(), draft.apiKey.trim());
      if (!options.qualityProfiles?.length || !options.rootFolders?.length) {
        throw new Error(`Add a quality profile and root folder in ${name}, then test again.`);
      }
      if (!options.qualityProfiles.some(profile => profile.id === draft.profileId)) {
        draft.profileId = options.qualityProfiles[0].id;
      }
      if (!options.rootFolders.some(folder => folder.path === draft.rootFolder)) {
        draft.rootFolder = options.rootFolders[0].path;
      }
      draft.options = options;
    } catch (err) {
      error = err instanceof Error ? err.message : `Could not connect to ${name}.`;
    } finally {
      busy = false;
    }
  }
</script>

<label for={`${service}-wizard-url`}>{name} Base URL</label>
<input id={`${service}-wizard-url`} type="url" bind:value={draft.baseUrl} on:input={invalidate} disabled={busy}
  placeholder={service === 'sonarr' ? 'http://localhost:8989' : 'http://localhost:7878'} />
<label for={`${service}-wizard-key`}>{name} API Key</label>
<input id={`${service}-wizard-key`} type="password" bind:value={draft.apiKey} on:input={invalidate} disabled={busy}
  placeholder={useSavedKey ? 'Use saved API key' : ''} autocomplete="off" />
<Button on:click={testConnection} disabled={busy || !draft.baseUrl.trim() || (!useSavedKey && !draft.apiKey.trim())}>
  {busy ? 'Testing...' : 'Test Connection'}
</Button>

{#if error}<Alert kind="danger">{error}</Alert>{/if}
{#if draft.options}
  <p role="status">Connected to {name}.</p>
  <label for={`${service}-wizard-profile`}>Quality Profile</label>
  <select id={`${service}-wizard-profile`} bind:value={draft.profileId}>
    {#each draft.options.qualityProfiles as profile}<option value={profile.id}>{profile.name}</option>{/each}
  </select>
  <label for={`${service}-wizard-folder`}>Root Folder</label>
  <select id={`${service}-wizard-folder`} bind:value={draft.rootFolder}>
    {#each draft.options.rootFolders as folder}<option value={folder.path}>{folder.path}</option>{/each}
  </select>
{/if}
