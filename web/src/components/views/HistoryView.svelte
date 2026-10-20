<script lang="ts">
  import { onMount } from 'svelte';
  import { RefreshCw, FileText } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Modal from '../common/Modal.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { SyncHistoryItem } from '../../lib/types';

  let historyItems: SyncHistoryItem[] = [];
  let loading = false;
  let errorMsg = '';

  // Error modal
  let selectedLog: SyncHistoryItem | null = null;
  let isLogModalOpen = false;

  function viewLog(item: SyncHistoryItem) {
    selectedLog = item;
    isLogModalOpen = true;
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      historyItems = await api.getSyncHistory();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load sync history';
    } finally {
      loading = false;
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

      {#if historyItems.length === 0}
        <div class="emptyState">
          <h3>No Synchronization History</h3>
          <p>No historical synchronization cycles have run on this instance yet.</p>
        </div>
      {:else}
        <Table>
          <thead>
            <tr>
              <th>Status</th>
              <th>Trigger</th>
              <th>Started At</th>
              <th>Completed At</th>
              <th>Total Titles</th>
              <th>Matched</th>
              <th>Unresolved</th>
              <th>Staged</th>
              <th style="text-align: right;">Details</th>
            </tr>
          </thead>
          <tbody>
            {#each historyItems as item (item.id)}
              <tr>
                <td>
                  <Label
                    kind={item.status === 'SUCCESS' ? 'success' : item.status === 'FAILED' ? 'danger' : 'warning'}
                    size="small"
                  >
                    {item.status}
                  </Label>
                </td>
                <td>
                  <span class="triggerBadge">{item.triggerType}</span>
                </td>
                <td>{new Date(item.startedAt).toLocaleString()}</td>
                <td>{item.completedAt ? new Date(item.completedAt).toLocaleString() : 'Running...'}</td>
                <td>{item.totalTitles}</td>
                <td>{item.matchedTitles}</td>
                <td>{item.unresolvedTitles}</td>
                <td>{item.stagedActions}</td>
                <td style="text-align: right;">
                  {#if item.errorLog}
                    <Button kind="default" size="small" on:click={() => viewLog(item)}>
                      <FileText size={12} /> Log
                    </Button>
                  {:else}
                    <span class="muted">-</span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </div>
  </div>

  <!-- Error / Details Modal -->
  <Modal
    isOpen={isLogModalOpen}
    title="Synchronization Execution Log #{selectedLog?.id}"
    size="medium"
    on:close={() => (isLogModalOpen = false)}
  >
    <div slot="body">
      {#if selectedLog?.errorLog}
        <pre class="logPre">{selectedLog.errorLog}</pre>
      {:else}
        <p>No error details recorded for this run.</p>
      {/if}
    </div>
    <div slot="footer">
      <Button kind="default" on:click={() => (isLogModalOpen = false)}>Close</Button>
    </div>
  </Modal>
</div>

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

  .triggerBadge {
    color: var(--textColor);
    font-size: var(--smallFontSize);
  }

  .muted {
    color: var(--disabledColor);
  }

  .logPre {
    background-color: var(--pageBackground);
    padding: 15px;
    border-radius: 4px;
    color: var(--dangerColor);
    font-family: var(--monoSpaceFontFamily);
    font-size: var(--smallFontSize);
    overflow-x: auto;
    white-space: pre-wrap;
    max-height: 400px;
    border: 1px solid var(--borderColor);
  }
</style>
