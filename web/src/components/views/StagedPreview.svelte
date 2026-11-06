<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { CheckSquare, Square, RefreshCw, Check, X } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import PageContentFooter from '../layout/PageContentFooter.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import CheckInput from '../common/CheckInput.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { StagedAction } from '../../lib/types';

  export let stagedActions: StagedAction[] = [];

  const dispatch = createEventDispatcher<{ refresh: void }>();

  let selectedIds = new Set<number>();
  let loading = false;
  let actionLoading = false;
  let errorMsg = '';
  let successMsg = '';

  $: if (stagedActions) {
    const validIds = new Set(stagedActions.map((a) => a.id));
    const pruned = [...selectedIds].filter((id) => validIds.has(id));
    if (pruned.length !== selectedIds.size) {
      selectedIds = new Set(pruned);
    }
  }

  $: allSelected = stagedActions.length > 0 && selectedIds.size === stagedActions.length;
  $: selectedCount = selectedIds.size;

  function selectAll() {
    selectedIds = new Set(stagedActions.map((a) => a.id));
  }

  function unselectAll() {
    selectedIds = new Set();
  }

  function toggleSelectAll() {
    if (allSelected) {
      unselectAll();
    } else {
      selectAll();
    }
  }

  function toggleRow(id: number) {
    const next = new Set(selectedIds);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    selectedIds = next;
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      stagedActions = await api.getStaged();
      // Drop any selected IDs that no longer exist
      const validIds = new Set(stagedActions.map((a) => a.id));
      selectedIds = new Set([...selectedIds].filter((id) => validIds.has(id)));
      dispatch('refresh');
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load staged actions';
    } finally {
      loading = false;
    }
  }

  async function applySelected() {
    if (selectedIds.size === 0 || actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      const ids = Array.from(selectedIds);
      const res = await api.applyStaged(ids);
      successMsg = `Applied ${res.appliedCount} staged action(s).`;
      selectedIds = new Set();
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to apply staged actions';
    } finally {
      actionLoading = false;
    }
  }

  async function rejectSelected() {
    if (selectedIds.size === 0 || actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      const ids = Array.from(selectedIds);
      const res = await api.rejectStaged(ids);
      successMsg = `Rejected ${res.rejectedCount} staged action(s).`;
      selectedIds = new Set();
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to reject staged actions';
    } finally {
      actionLoading = false;
    }
  }

  async function applySingle(id: number) {
    if (actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    try {
      await api.applyStaged([id]);
      selectedIds.delete(id);
      selectedIds = new Set(selectedIds);
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to apply action';
    } finally {
      actionLoading = false;
    }
  }

  async function rejectSingle(id: number) {
    if (actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    try {
      await api.rejectStaged([id]);
      selectedIds.delete(id);
      selectedIds = new Set(selectedIds);
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to reject action';
    } finally {
      actionLoading = false;
    }
  }

  function formatPayload(payload: any): string {
    if (!payload) return '-';
    if (typeof payload === 'string') return payload;
    try {
      return JSON.stringify(payload);
    } catch {
      return String(payload);
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
        label="Select All"
        disabled={stagedActions.length === 0}
        on:click={selectAll}
      >
        <CheckSquare size={18} />
      </PageToolbarButton>

      <PageToolbarButton
        label="Unselect All"
        disabled={selectedIds.size === 0}
        on:click={unselectAll}
      >
        <Square size={18} />
      </PageToolbarButton>

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

      {#if stagedActions.length === 0}
        <div class="emptyState">
          <h3>No Staged Actions</h3>
          <p>There are no pending synchronization actions awaiting dry-run confirmation.</p>
        </div>
      {:else}
        <Table>
          <thead>
            <tr>
              <th style="width: 40px; text-align: center;">
                <CheckInput checked={allSelected} on:change={toggleSelectAll} ariaLabel="Select all staged actions" />
              </th>
              <th>Action</th>
              <th>Type</th>
              <th>Title</th>
              <th>Service</th>
              <th>Payload Details</th>
              <th>Created</th>
              <th style="text-align: right; width: 140px;">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each stagedActions as action (action.id)}
              <tr class={selectedIds.has(action.id) ? 'selectedRow' : ''}>
                <td style="text-align: center;">
                  <CheckInput
                    checked={selectedIds.has(action.id)}
                    on:change={() => toggleRow(action.id)}
                    ariaLabel="Select action {action.id}"
                  />
                </td>
                <td>
                  <Label
                    kind={action.actionType.startsWith('ADD') ? 'success' : (action.actionType.startsWith('UNMONITOR') || action.actionType.startsWith('DELETE')) ? 'danger' : 'info'}
                    size="small"
                  >
                    {action.actionType}
                  </Label>
                </td>
                <td>
                  <Label kind={action.mediaType === 'SERIES' ? 'primary' : 'info'} size="small">
                    {action.mediaType}
                  </Label>
                </td>
                <td class="boldTitle">{action.title}</td>
                <td>
                  <span class="serviceBadge">{action.targetService || action.service || '-'}</span>
                </td>
                <td class="payloadCell" title={formatPayload(action.payloadJson ?? action.payload)}>
                  <code>{formatPayload(action.payloadJson ?? action.payload)}</code>
                </td>
                <td class="timestamp">{new Date(action.createdAt).toLocaleTimeString()}</td>
                <td style="text-align: right;">
                  <div class="rowActions">
                    <Button kind="success" size="small" on:click={() => applySingle(action.id)} disabled={actionLoading}>
                      <Check size={12} /> Apply
                    </Button>
                    <Button kind="danger" size="small" on:click={() => rejectSingle(action.id)} disabled={actionLoading}>
                      <X size={12} /> Reject
                    </Button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </div>
  </div>

  {#if stagedActions.length > 0}
    <PageContentFooter>
      <div class="actionButtons">
        <Button
          kind="success"
          size="medium"
          disabled={selectedCount === 0 || actionLoading}
          on:click={applySelected}
        >
          <Check size={16} /> Approve Selected
        </Button>
        <Button
          kind="danger"
          size="medium"
          disabled={selectedCount === 0 || actionLoading}
          on:click={rejectSelected}
        >
          <X size={16} /> Reject Selected
        </Button>
      </div>
      <div class="selected">
        <strong>{selectedCount} actions selected</strong>
      </div>
    </PageContentFooter>
  {/if}
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

  .boldTitle {
    font-weight: 500;
    color: var(--white);
  }

  .serviceBadge {
    font-size: var(--smallFontSize);
    color: var(--textColor);
  }

  .payloadCell {
    max-width: 280px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--monoSpaceFontFamily);
    font-size: var(--smallFontSize);
    color: var(--helpTextColor);
  }

  .timestamp {
    font-size: var(--smallFontSize);
    color: var(--helpTextColor);
  }

  .rowActions {
    display: inline-flex;
    gap: 6px;
    justify-content: flex-end;
  }

  .actionButtons {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .selected {
    font-size: var(--defaultFontSize);
    color: var(--white);
  }

  .selectedRow {
    background-color: var(--inputSelectedBackgroundColor);
  }
</style>
