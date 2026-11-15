<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { RefreshCw, Trash2, ExternalLink } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Modal from '../common/Modal.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { IgnoredItem } from '../../lib/types';

  export let ignoredItems: IgnoredItem[] = [];

  const dispatch = createEventDispatcher<{ refresh: void }>();

  let loading = false;
  let deleting = false;
  let errorMsg = '';
  let successMsg = '';

  let isDeleteModalOpen = false;
  let itemToDelete: IgnoredItem | null = null;

  function openDeleteModal(item: IgnoredItem) {
    itemToDelete = item;
    isDeleteModalOpen = true;
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      ignoredItems = await api.getIgnored();
      dispatch('refresh');
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load blocklist';
    } finally {
      loading = false;
    }
  }

  async function handleDelete() {
    if (!itemToDelete || deleting) return;
    deleting = true;
    errorMsg = '';
    successMsg = '';
    try {
      await api.deleteIgnored(itemToDelete.anilistId);
      successMsg = `Removed "${itemToDelete.titleRomaji}" from blocklist. The next sync cycle will evaluate this title again.`;
      isDeleteModalOpen = false;
      itemToDelete = null;
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to remove title from blocklist';
    } finally {
      deleting = false;
    }
  }

  function formatDate(dateStr: string): string {
    if (!dateStr) return '';
    try {
      const d = new Date(dateStr);
      return isNaN(d.getTime()) ? dateStr : d.toLocaleDateString();
    } catch {
      return dateStr;
    }
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="viewContainer">
  <PageToolbar>
    <svelte:fragment slot="left">
      <PageToolbarButton
        label="Refresh"
        disabled={loading}
        on:click={loadData}
      >
        <RefreshCw size={18} class={loading ? 'spinning' : ''} />
      </PageToolbarButton>
    </svelte:fragment>
  </PageToolbar>

  <div class="body">
    <div class="innerContentBody">
      {#if errorMsg}
        <Alert kind="danger">{errorMsg}</Alert>
      {/if}

      {#if successMsg}
        <Alert kind="success">{successMsg}</Alert>
      {/if}

      {#if ignoredItems.length === 0}
        <div class="emptyState">
          <h3>Blocklist Clear</h3>
          <p>No anime titles are currently ignored or blocked from synchronization.</p>
        </div>
      {:else}
        <Table>
          <thead>
            <tr>
              <th style="width: 140px;">AniList ID</th>
              <th>Title</th>
              <th style="width: 220px;">Reason</th>
              <th style="width: 150px;">Date Added</th>
              <th class="actionCol" style="width: 90px;">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each ignoredItems as item (item.anilistId)}
              <tr>
                <td>
                  <a
                    href="https://anilist.co/anime/{item.anilistId}"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="linkBadge"
                  >
                    <Label kind="default" size="small">
                      #{item.anilistId} <ExternalLink size={10} />
                    </Label>
                  </a>
                </td>
                <td class="titleCell">{item.titleRomaji}</td>
                <td>
                  <Label kind="warning" size="small">
                    {item.reason || 'Ignored'}
                  </Label>
                </td>
                <td>{formatDate(item.createdAt)}</td>
                <td class="actionCol">
                  <button
                    type="button"
                    class="actionBtn deleteBtn"
                    on:click={() => openDeleteModal(item)}
                    title="Remove from Blocklist (Unignore)"
                  >
                    <Trash2 size={16} />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </div>
  </div>
</div>

<!-- Delete Confirmation Modal -->
<Modal
  isOpen={isDeleteModalOpen}
  title="Remove from Blocklist"
  on:close={() => (isDeleteModalOpen = false)}
>
  {#if itemToDelete}
    <p>
      Are you sure you want to remove <strong>{itemToDelete.titleRomaji}</strong> (AniList #{itemToDelete.anilistId}) from the blocklist?
    </p>
    <p class="modalHelpText">
      The title will be eligible for matching on the next synchronization cycle.
    </p>
  {/if}

  <svelte:fragment slot="footer">
    <Button
      kind="default"
      on:click={() => (isDeleteModalOpen = false)}
      disabled={deleting}
    >
      Cancel
    </Button>
    <Button
      kind="danger"
      on:click={handleDelete}
      disabled={deleting}
    >
      Remove from Blocklist
    </Button>
  </svelte:fragment>
</Modal>

<style>
  .viewContainer {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    overflow: hidden;
  }

  .body {
    flex: 1 0 1px;
    overflow-y: auto;
  }

  .innerContentBody {
    padding: var(--pageContentBodyPadding);
  }

  .emptyState {
    text-align: center;
    padding: 60px 20px;
    color: var(--disabledColor);
  }

  .emptyState h3 {
    color: var(--textColor);
    font-size: 20px;
    margin-bottom: 8px;
  }

  .linkBadge {
    display: inline-flex;
    text-decoration: none;
  }

  .titleCell {
    font-weight: 500;
    color: var(--white);
  }

  .actionCol {
    text-align: right;
  }

  .actionBtn {
    background: transparent;
    border: none;
    color: var(--defaultButtonTextColor);
    cursor: pointer;
    padding: 6px;
    border-radius: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: background-color 150ms ease, color 150ms ease;
  }

  .actionBtn:hover {
    background-color: var(--defaultHoverBackgroundColor);
    color: var(--white);
  }

  .deleteBtn:hover {
    background-color: var(--dangerColor);
    color: var(--white);
  }

  .modalHelpText {
    color: var(--helpTextColor);
    font-size: var(--smallFontSize);
    margin-top: 8px;
  }
</style>
