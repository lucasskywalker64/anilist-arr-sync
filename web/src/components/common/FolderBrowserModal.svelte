<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { Folder, CornerLeftUp, HardDrive, ArrowRight, AlertCircle } from 'lucide-svelte';
  import Modal from './Modal.svelte';
  import Button from './Button.svelte';
  import Alert from './Alert.svelte';
  import { api } from '../../lib/api';
  import type { FilesystemDirectory } from '../../lib/types';

  export let isOpen = false;
  export let initialPath = '';

  const dispatch = createEventDispatcher<{
    select: string;
    close: void;
  }>();

  let inputPath = '';
  let currentPath = '';
  let parentPath = '';
  let roots: string[] = [];
  let directories: FilesystemDirectory[] = [];
  let loading = false;
  let errorMsg = '';

  let lastOpenState = false;

  $: if (isOpen && !lastOpenState) {
    lastOpenState = true;
    inputPath = initialPath || '';
    navigate(initialPath || undefined);
  } else if (!isOpen) {
    lastOpenState = false;
  }

  let navigationId = 0;

  async function navigate(targetPath?: string) {
    const requestId = ++navigationId;
    loading = true;
    errorMsg = '';
    try {
      const res = await api.getFilesystem(targetPath);
      if (requestId !== navigationId) return;
      currentPath = res.current;
      inputPath = res.current;
      parentPath = res.parent || '';
      roots = res.roots || [];
      directories = res.directories || [];
    } catch (e: any) {
      if (requestId !== navigationId) return;
      errorMsg = e.message || 'Failed to navigate directory';
      currentPath = '';
    } finally {
      if (requestId === navigationId) {
        loading = false;
      }
    }
  }

  function handlePathSubmit() {
    if (!inputPath.trim()) return;
    navigate(inputPath.trim());
  }

  function handlePathKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      handlePathSubmit();
    }
  }

  function handleSelect() {
    if (!currentPath) return;
    dispatch('select', currentPath);
    dispatch('close');
  }

  function handleClose() {
    dispatch('close');
  }
</script>

<Modal
  {isOpen}
  title="File Browser"
  size="large"
  on:close={handleClose}
>
  <div slot="body" class="browserBody">
    <div class="searchBar">
      <input
        type="text"
        class="pathInput"
        placeholder="Enter path or select below"
        bind:value={inputPath}
        on:keydown={handlePathKeydown}
      />
      <Button kind="primary" on:click={handlePathSubmit} disabled={loading}>
        <ArrowRight size={14} /> Go
      </Button>
    </div>

    {#if roots.length > 0}
      <div class="rootsBar">
        <span class="rootsLabel">Drives / Mounts:</span>
        <div class="rootsList">
          {#each roots as root}
            <button
              type="button"
              class="rootButton {currentPath.startsWith(root) ? 'activeRoot' : ''}"
              on:click={() => navigate(root)}
              disabled={loading}
            >
              <HardDrive size={13} /> {root}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    {#if errorMsg}
      <div style="margin-top: 10px;">
        <Alert kind="danger">{errorMsg}</Alert>
      </div>
    {/if}

    <div class="browserTableContainer">
      {#if loading}
        <div class="stateNotice">Loading directory...</div>
      {:else}
        <table class="browserTable">
          <thead>
            <tr>
              <th style="width: 50px;">Type</th>
              <th>Name</th>
            </tr>
          </thead>
          <tbody>
            {#if parentPath}
              <tr
                class="folderRow"
                on:click={() => navigate(parentPath)}
                tabindex="0"
                role="button"
                on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && navigate(parentPath)}
              >
                <td class="typeCol"><CornerLeftUp size={16} /></td>
                <td class="nameCol">..</td>
              </tr>
            {/if}
            {#each directories as dir}
              <tr
                class="folderRow"
                on:click={() => navigate(dir.path)}
                tabindex="0"
                role="button"
                on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && navigate(dir.path)}
              >
                <td class="typeCol"><Folder size={16} /></td>
                <td class="nameCol">{dir.name}</td>
              </tr>
            {/each}
            {#if directories.length === 0}
              <tr>
                <td colspan="2" class="stateNotice">No subfolders found in this directory.</td>
              </tr>
            {/if}
          </tbody>
        </table>
      {/if}
    </div>
  </div>

  <div slot="footer" class="browserFooter">
    <Button kind="default" on:click={handleClose}>
      Cancel
    </Button>
    <Button kind="primary" on:click={handleSelect} disabled={!currentPath || loading}>
      Ok
    </Button>
  </div>
</Modal>

<style>
  .browserBody {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .searchBar {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .pathInput {
    flex: 1;
    background-color: var(--inputBackgroundColor, #1e1e1e);
    color: var(--textColor, #e1e1e1);
    border: 1px solid var(--inputBorderColor, #3a3a3a);
    padding: 8px 12px;
    border-radius: 3px;
    font-size: var(--defaultFontSize, 13px);
    outline: none;
  }

  .pathInput:focus {
    border-color: var(--themeAccent, #5d9cec);
  }

  .rootsBar {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    font-size: 12px;
  }

  .rootsLabel {
    color: var(--dimTextColor, #8c8c8c);
    font-weight: 500;
  }

  .rootsList {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .rootButton {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    background-color: var(--cardBackgroundColor, #252525);
    color: var(--textColor, #e1e1e1);
    border: 1px solid var(--borderColor, #3a3a3a);
    padding: 4px 8px;
    border-radius: 3px;
    font-size: 12px;
    cursor: pointer;
    transition: background-color 150ms ease, border-color 150ms ease;
  }

  .rootButton:hover:not(:disabled) {
    background-color: var(--tableRowHoverBackgroundColor, #333333);
    border-color: var(--themeAccent, #5d9cec);
  }

  .rootButton.activeRoot {
    border-color: var(--themeAccent, #5d9cec);
    color: var(--themeAccent, #5d9cec);
  }

  .browserTableContainer {
    max-height: 380px;
    min-height: 220px;
    overflow-y: auto;
    border: 1px solid var(--borderColor, #3a3a3a);
    border-radius: 3px;
    background-color: var(--cardBackgroundColor, #1a1a1a);
  }

  .browserTable {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--defaultFontSize, 13px);
  }

  .browserTable th {
    position: sticky;
    top: 0;
    background-color: var(--cardBackgroundColor, #222222);
    padding: 8px 12px;
    text-align: left;
    font-weight: 600;
    color: var(--textColor, #e1e1e1);
    border-bottom: 1px solid var(--borderColor, #3a3a3a);
    z-index: 1;
  }

  .browserTable td {
    padding: 8px 12px;
    border-bottom: 1px solid var(--borderColor, #2a2a2a);
    vertical-align: middle;
  }

  .folderRow {
    cursor: pointer;
    transition: background-color 100ms ease;
  }

  .folderRow:hover {
    background-color: var(--tableRowHoverBackgroundColor, #2e2e2e);
  }

  .folderRow:focus-visible {
    outline: 2px solid var(--themeAccent, #5d9cec);
  }

  .typeCol {
    color: var(--themeAccent, #5d9cec);
    width: 50px;
  }

  .nameCol {
    color: var(--textColor, #e1e1e1);
    font-weight: 500;
  }

  .stateNotice {
    padding: 24px 16px;
    text-align: center;
    color: var(--dimTextColor, #8c8c8c);
    font-style: italic;
  }

  .browserFooter {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
