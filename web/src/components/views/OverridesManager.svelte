<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { Plus, RefreshCw, Edit, Trash2, ExternalLink, Ban } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Modal from '../common/Modal.svelte';
  import FormGroup from '../common/FormGroup.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { OverrideItem } from '../../lib/types';

  export let overrides: OverrideItem[] = [];

  const dispatch = createEventDispatcher<{
    refresh: void;
    navigate: string;
  }>();

  let loading = false;
  let errorMsg = '';
  let successMsg = '';

  // Modal state
  let isModalOpen = false;
  let isDeleteModalOpen = false;
  let editingOverride: Partial<OverrideItem> | null = null;
  let overrideToDelete: OverrideItem | null = null;
  let formSaving = false;
  let formError = '';

  // Form fields
  let formAnilistId = '';
  let formMediaType: 'SERIES' | 'MOVIE' = 'SERIES';
  let formTvdbId = '';
  let formTmdbId = '';
  let formSeasons = '1';
  let formTitleOverride = '';

  function openAddModal() {
    editingOverride = null;
    formAnilistId = '';
    formMediaType = 'SERIES';
    formTvdbId = '';
    formTmdbId = '';
    formSeasons = '1';
    formTitleOverride = '';
    formError = '';
    isModalOpen = true;
  }

  function openEditModal(item: OverrideItem) {
    editingOverride = item;
    formAnilistId = String(item.anilistId);
    formMediaType = item.mediaType;
    formTvdbId = item.tvdbId != null ? String(item.tvdbId) : '';
    formTmdbId = item.tmdbId != null ? String(item.tmdbId) : '';
    formSeasons = item.seasons || '1';
    formTitleOverride = item.titleOverride || '';
    formError = '';
    isModalOpen = true;
  }

  function openDeleteModal(item: OverrideItem) {
    overrideToDelete = item;
    isDeleteModalOpen = true;
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      overrides = await api.getOverrides();
      dispatch('refresh');
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load overrides';
    } finally {
      loading = false;
    }
  }

  async function handleSaveOverride() {
    const anilistIdNum = parseInt(formAnilistId.trim(), 10);
    if (!anilistIdNum || isNaN(anilistIdNum)) {
      formError = 'AniList ID must be a positive integer';
      return;
    }

    formSaving = true;
    formError = '';
    try {
      const payload: Partial<OverrideItem> = {
        anilistId: anilistIdNum,
        mediaType: formMediaType,
        tvdbId: formTvdbId.trim() ? parseInt(formTvdbId.trim(), 10) : null,
        tmdbId: formTmdbId.trim() ? parseInt(formTmdbId.trim(), 10) : null,
        seasons: formSeasons.trim() || '1',
        titleOverride: formTitleOverride.trim() || null,
      };

      await api.createOverride(payload);
      isModalOpen = false;
      successMsg = `Override for AniList #${anilistIdNum} saved successfully.`;
      await loadData();
    } catch (e: any) {
      formError = e.message || 'Failed to save override';
    } finally {
      formSaving = false;
    }
  }

  async function handleDeleteConfirm() {
    if (!overrideToDelete) return;
    formSaving = true;
    try {
      await api.deleteOverride(overrideToDelete.id);
      isDeleteModalOpen = false;
      successMsg = `Override for AniList #${overrideToDelete.anilistId} removed.`;
      overrideToDelete = null;
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to delete override';
    } finally {
      formSaving = false;
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
        label="Add Override"
        on:click={openAddModal}
      >
        <Plus size={18} />
      </PageToolbarButton>

      <PageToolbarButton
        label="Blocklist"
        on:click={() => dispatch('navigate', '/activity/blocklist')}
      >
        <Ban size={18} />
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

      {#if overrides.length === 0}
        <div class="emptyState">
          <h3>No Manual Overrides Configured</h3>
          <p>Manual overrides force explicit mappings from an AniList anime to specific TVDB/TMDB records and season numbers.</p>
          <div style="margin-top: 20px;">
            <Button kind="primary" on:click={openAddModal}>
              <Plus size={16} /> Add First Override
            </Button>
          </div>
        </div>
      {:else}
        <Table>
          <thead>
            <tr>
              <th>AniList ID</th>
              <th>Media Type</th>
              <th>TVDB ID</th>
              <th>TMDB ID</th>
              <th>Seasons</th>
              <th>Title Override</th>
              <th>Updated At</th>
              <th style="text-align: right; width: 100px;">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each overrides as item (item.id)}
              <tr>
                <td>
                  <a
                    href="https://anilist.co/anime/{item.anilistId}"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="anilistLink"
                  >
                    #{item.anilistId} <ExternalLink size={12} />
                  </a>
                </td>
                <td>
                  <Label kind={item.mediaType === 'SERIES' ? 'primary' : 'info'} size="small">
                    {item.mediaType}
                  </Label>
                </td>
                <td>{item.tvdbId ?? '-'}</td>
                <td>{item.tmdbId ?? '-'}</td>
                <td><code>{item.seasons || '1'}</code></td>
                <td>{item.titleOverride || '-'}</td>
                <td class="timestamp">{new Date(item.updatedAt).toLocaleDateString()}</td>
                <td style="text-align: right;">
                  <div class="actionButtons">
                    <button class="iconBtn" on:click={() => openEditModal(item)} title="Edit override">
                      <Edit size={16} />
                    </button>
                    <button class="iconBtn dangerIcon" on:click={() => openDeleteModal(item)} title="Delete override">
                      <Trash2 size={16} />
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </div>
  </div>

  <!-- Add / Edit Override Modal -->
  <Modal
    isOpen={isModalOpen}
    title={editingOverride ? `Edit Override #${editingOverride.anilistId}` : 'Add Manual Override'}
    size="medium"
    on:close={() => (isModalOpen = false)}
  >
    <div slot="body">
      {#if formError}
        <Alert kind="danger">{formError}</Alert>
      {/if}

      <FormGroup label="AniList ID" helpText="The numeric AniList anime ID (e.g. 16498)">
        <input
          type="number"
          class="formInput"
          placeholder="16498"
          bind:value={formAnilistId}
          disabled={formSaving}
        />
      </FormGroup>

      <FormGroup label="Media Type" helpText="Select SERIES for Sonarr or MOVIE for Radarr">
        <select class="formInput" bind:value={formMediaType} disabled={formSaving}>
          <option value="SERIES">SERIES (Sonarr)</option>
          <option value="MOVIE">MOVIE (Radarr)</option>
        </select>
      </FormGroup>

      {#if formMediaType === 'SERIES'}
        <FormGroup label="TVDB ID" helpText="Target TheTVDB series ID">
          <input
            type="number"
            class="formInput"
            placeholder="e.g. 267440"
            bind:value={formTvdbId}
            disabled={formSaving}
          />
        </FormGroup>

        <FormGroup label="Seasons" helpText="Comma-separated season numbers to monitor (e.g. 1 or 1,2)">
          <input
            type="text"
            class="formInput"
            placeholder="1"
            bind:value={formSeasons}
            disabled={formSaving}
          />
        </FormGroup>
      {:else}
        <FormGroup label="TMDB ID" helpText="Target TMDb movie ID">
          <input
            type="number"
            class="formInput"
            placeholder="e.g. 372058"
            bind:value={formTmdbId}
            disabled={formSaving}
          />
        </FormGroup>
      {/if}

      <FormGroup label="Title Override" helpText="Optional title string to force in searches">
        <input
          type="text"
          class="formInput"
          placeholder="Optional override title"
          bind:value={formTitleOverride}
          disabled={formSaving}
        />
      </FormGroup>
    </div>

    <div slot="footer">
      <Button kind="default" on:click={() => (isModalOpen = false)} disabled={formSaving}>
        Cancel
      </Button>
      <Button kind="primary" on:click={handleSaveOverride} disabled={formSaving}>
        {formSaving ? 'Saving...' : 'Save Override'}
      </Button>
    </div>
  </Modal>

  <!-- Delete Confirmation Modal -->
  <Modal
    isOpen={isDeleteModalOpen}
    title="Delete Manual Override"
    size="small"
    on:close={() => (isDeleteModalOpen = false)}
  >
    <div slot="body">
      <p>
        Are you sure you want to delete the manual override for
        <strong>AniList #{overrideToDelete?.anilistId}</strong>?
      </p>
      <p class="warningText">
        Future synchronization runs will revert to standard resolver lookup for this title.
      </p>
    </div>

    <div slot="footer">
      <Button kind="default" on:click={() => (isDeleteModalOpen = false)} disabled={formSaving}>
        Cancel
      </Button>
      <Button kind="danger" on:click={handleDeleteConfirm} disabled={formSaving}>
        {formSaving ? 'Deleting...' : 'Delete'}
      </Button>
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

  .anilistLink {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-weight: 500;
  }

  .timestamp {
    font-size: var(--smallFontSize);
    color: var(--helpTextColor);
  }

  .actionButtons {
    display: inline-flex;
    gap: 8px;
    justify-content: flex-end;
  }

  .iconBtn {
    background: transparent;
    border: none;
    color: var(--iconButtonHoverLightColor);
    cursor: pointer;
    padding: 4px;
    border-radius: 3px;
    transition: color 150ms ease, background-color 150ms ease;
  }

  .iconBtn:hover {
    color: var(--themeBlue);
    background-color: rgba(255, 255, 255, 0.05);
  }

  .dangerIcon:hover {
    color: var(--dangerColor);
  }

  .warningText {
    color: var(--helpTextColor);
    font-size: var(--smallFontSize);
    margin-top: 10px;
  }
</style>
