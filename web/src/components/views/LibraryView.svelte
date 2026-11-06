<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import {
    RefreshCw,
    Search,
    LayoutGrid,
    List,
    ExternalLink,
    SlidersHorizontal,
    Tv,
    Film,
    CheckCircle,
    XCircle,
    AlertTriangle,
    EyeOff,
  } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Alert from '../common/Alert.svelte';
  import Modal from '../common/Modal.svelte';
  import { api } from '../../lib/api';
  import type { LibraryItem, LibraryStats, OverrideItem } from '../../lib/types';

  const dispatch = createEventDispatcher<{
    navigate: string;
    refresh: void;
  }>();

  let libraryItems: LibraryItem[] = [];
  export let stats: LibraryStats | null = null;
  export let completedSyncRunId = 0;
  let seenSyncRunId = completedSyncRunId;
  $: if (completedSyncRunId !== seenSyncRunId) {
    seenSyncRunId = completedSyncRunId;
    loadData();
  }
  let loading = false;
  let itemsError = false;
  let errorMsg = '';
  let successMsg = '';

  // View preferences
  let viewMode: 'grid' | 'table' = 'grid';
  if (typeof localStorage !== 'undefined') {
    const saved = localStorage.getItem('anilist_arr_sync_library_view');
    if (saved === 'table' || saved === 'grid') {
      viewMode = saved;
    }
  }

  function setViewMode(mode: 'grid' | 'table') {
    viewMode = mode;
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem('anilist_arr_sync_library_view', mode);
    }
  }

  // Filters & Search
  let statusFilter = 'all';
  let serviceFilter = 'all';
  let monitoredFilter = 'all';
  let searchTerm = '';
  let sortBy = 'title';
  let sortOrder = 'asc';

  // Image load error handling
  let failedImages = new Set<number>();
  function handleImageError(id: number) {
    failedImages.add(id);
    failedImages = new Set(failedImages);
  }

  // Quick Override Modal
  let isOverrideModalOpen = false;
  let overrideItem: LibraryItem | null = null;
  let overrideMediaType: 'SERIES' | 'MOVIE' = 'SERIES';
  let overrideTvdbId = '';
  let overrideTmdbId = '';
  let overrideSeasons = '1';
  let overrideTitle = '';
  let overrideSaving = false;

  function openOverrideModal(item: LibraryItem) {
    overrideItem = item;
    overrideMediaType = item.mediaType || 'SERIES';
    overrideTvdbId = item.tvdbId ? String(item.tvdbId) : '';
    overrideTmdbId = item.tmdbId ? String(item.tmdbId) : '';
    overrideSeasons = item.seasons || '1';
    overrideTitle = item.arrTitle || '';
    isOverrideModalOpen = true;
  }

  // Request sequence & search debounce
  let loadSeq = 0;
  let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;

  onDestroy(() => {
    clearSearchDebounce();
  });

  function clearSearchDebounce() {
    if (searchDebounceTimer) {
      clearTimeout(searchDebounceTimer);
      searchDebounceTimer = null;
    }
  }

  function handleSearchInput() {
    clearSearchDebounce();
    searchDebounceTimer = setTimeout(() => {
      loadData();
    }, 250);
  }

  async function saveOverride() {
    if (!overrideItem) return;
    overrideSaving = true;
    errorMsg = '';
    const tvdbStr = String(overrideTvdbId ?? '').trim();
    const tmdbStr = String(overrideTmdbId ?? '').trim();
    const seasonsStr = String(overrideSeasons ?? '').trim();
    const titleStr = String(overrideTitle ?? '').trim();

    try {
      const payload: Partial<OverrideItem> = {
        anilistId: overrideItem.anilistId,
        mediaType: overrideMediaType,
        tvdbId: tvdbStr ? parseInt(tvdbStr, 10) : null,
        tmdbId: tmdbStr ? parseInt(tmdbStr, 10) : null,
        seasons: seasonsStr || '1',
        titleOverride: titleStr || undefined,
      };
      await api.createOverride(payload);
      isOverrideModalOpen = false;
      successMsg = `Override saved for ${overrideItem.titleRomaji}`;
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to save override';
    } finally {
      overrideSaving = false;
    }
  }

  async function loadData() {
    const seq = ++loadSeq;
    loading = true;
    errorMsg = '';
    itemsError = false;
    try {
      const [itemsRes, statsRes] = await Promise.allSettled([
        api.getLibrary({
          status: statusFilter,
          service: serviceFilter,
          monitored: monitoredFilter,
          search: searchTerm,
          sort: sortBy,
          order: sortOrder,
        }),
        api.getLibraryStats(),
      ]);
      if (seq !== loadSeq) return;

      if (itemsRes.status === 'fulfilled') {
        libraryItems = itemsRes.value;
      } else {
        libraryItems = [];
        itemsError = true;
        errorMsg = itemsRes.reason?.message || 'Failed to load library items';
      }

      if (statsRes.status === 'fulfilled') {
        stats = statsRes.value;
      } else {
        stats = null;
      }
    } catch (e: any) {
      if (seq !== loadSeq) return;
      itemsError = true;
      errorMsg = e.message || 'Failed to load library';
    } finally {
      if (seq === loadSeq) {
        loading = false;
      }
    }
  }

  function handleFilterChange() {
    clearSearchDebounce();
    loadData();
  }

  function formatStatusLabel(status: string): string {
    switch (status) {
      case 'CURRENT':
        return 'Watching';
      case 'PLANNING':
        return 'Planning';
      case 'COMPLETED':
        return 'Completed';
      case 'DROPPED':
        return 'Dropped';
      case 'PAUSED':
        return 'Paused';
      default:
        return status;
    }
  }

  function getStatusKind(status: string): 'success' | 'info' | 'primary' | 'danger' | 'warning' | 'default' {
    switch (status) {
      case 'CURRENT':
        return 'success';
      case 'PLANNING':
        return 'info';
      case 'COMPLETED':
        return 'primary';
      case 'DROPPED':
        return 'danger';
      case 'PAUSED':
        return 'warning';
      default:
        return 'default';
    }
  }

  function getMonitoredKind(status: string): 'success' | 'warning' | 'danger' | 'default' {
    switch (status) {
      case 'MONITORED':
        return 'success';
      case 'PENDING_REVIEW':
        return 'warning';
      case 'IGNORED':
        return 'danger';
      default:
        return 'default';
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

      <!-- Search Input -->
      <div class="searchWrapper">
        <Search size={14} class="searchIcon" />
        <input
          type="text"
          placeholder="Filter anime..."
          bind:value={searchTerm}
          on:input={handleSearchInput}
          class="searchInput"
        />
        {#if searchTerm}
          <button
            type="button"
            class="clearBtn"
            on:click={() => { clearSearchDebounce(); searchTerm = ''; handleFilterChange(); }}
          >
            ×
          </button>
        {/if}
      </div>

      <!-- AniList Watch Status Filter -->
      <select
        bind:value={statusFilter}
        on:change={handleFilterChange}
        class="toolbarSelect"
        title="Filter by AniList watch status"
      >
        <option value="all">Watch: All</option>
        <option value="CURRENT">Watching</option>
        <option value="PLANNING">Planning</option>
        <option value="COMPLETED">Completed</option>
        <option value="DROPPED">Dropped</option>
        <option value="PAUSED">Paused</option>
      </select>

      <!-- Target Service Filter -->
      <select
        bind:value={serviceFilter}
        on:change={handleFilterChange}
        class="toolbarSelect"
        title="Filter by Arr service"
      >
        <option value="all">Service: All</option>
        <option value="SONARR">Sonarr</option>
        <option value="RADARR">Radarr</option>
      </select>

      <!-- Monitored Status Filter -->
      <select
        bind:value={monitoredFilter}
        on:change={handleFilterChange}
        class="toolbarSelect"
        title="Filter by monitoring state"
      >
        <option value="all">State: All</option>
        <option value="MONITORED">Monitored</option>
        <option value="UNMONITORED">Unmonitored</option>
        <option value="PENDING_REVIEW">Pending Review</option>
        <option value="IGNORED">Ignored</option>
      </select>
    </svelte:fragment>

    <svelte:fragment slot="right">
      <!-- Sort Order -->
      <select
        bind:value={sortBy}
        on:change={handleFilterChange}
        class="toolbarSelect"
        title="Sort library"
      >
        <option value="title">Sort: Title</option>
        <option value="year">Sort: Year</option>
        <option value="status">Sort: Status</option>
        <option value="synced">Sort: Last Synced</option>
      </select>

      <!-- View Mode Toggle -->
      <div class="viewToggle">
        <button
          type="button"
          class="toggleBtn {viewMode === 'grid' ? 'active' : ''}"
          title="Poster Grid View"
          on:click={() => setViewMode('grid')}
        >
          <LayoutGrid size={16} />
        </button>
        <button
          type="button"
          class="toggleBtn {viewMode === 'table' ? 'active' : ''}"
          title="Table View"
          on:click={() => setViewMode('table')}
        >
          <List size={16} />
        </button>
      </div>

      {#if stats && !itemsError}
        <div class="statsBadge" title="Total synchronized library count">
          {libraryItems.length} of {stats.total}
        </div>
        <div class="statsBadge" title="Titles with CURRENT AniList status">
          {stats.watchingCount} watching
        </div>
      {/if}
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

      {#if !loading && !itemsError && libraryItems.length === 0}
        <div class="emptyState">
          <h3>No Anime in Library</h3>
          {#if searchTerm || statusFilter !== 'all' || serviceFilter !== 'all' || monitoredFilter !== 'all'}
            <p>No titles matched your selected search and filter criteria.</p>
            <Button kind="default" size="small" on:click={() => {
              clearSearchDebounce();
              searchTerm = '';
              statusFilter = 'all';
              serviceFilter = 'all';
              monitoredFilter = 'all';
              handleFilterChange();
            }}>
              Clear Filters
            </Button>
          {:else}
            <p>Run a synchronization pass from the Dashboard to populate your tracked AniList library.</p>
            <Button kind="primary" on:click={() => dispatch('navigate', '/')}>
              Go to Dashboard
            </Button>
          {/if}
        </div>
      {:else if viewMode === 'grid'}
        <!-- POSTER GRID VIEW (Sonarr Style) -->
        <div class="posterGrid">
          {#each libraryItems as item (item.anilistId)}
            <div class="card">
              <!-- Top Image Box -->
              <div class="posterWrapper">
                {#if item.posterUrl && !failedImages.has(item.anilistId)}
                  <img
                    src={item.posterUrl}
                    alt={item.titleRomaji}
                    class="posterImg"
                    on:error={() => handleImageError(item.anilistId)}
                  />
                {:else}
                  <div class="posterFallback">
                    {#if item.mediaType === 'SERIES'}
                      <Tv size={36} />
                    {:else}
                      <Film size={36} />
                    {/if}
                  </div>
                {/if}

                <!-- Top Left: Service Badge -->
                <div class="servicePill">
                  {#if item.targetService === 'SONARR'}
                    <span class="pill sonarrPill">Sonarr</span>
                  {:else if item.targetService === 'RADARR'}
                    <span class="pill radarrPill">Radarr</span>
                  {:else}
                    <span class="pill unmappedPill">Unmapped</span>
                  {/if}
                </div>

                <!-- Top Right: Monitored State -->
                <div class="monitoredPill">
                  {#if item.monitoredStatus === 'MONITORED'}
                    <span class="stateBadge monitored" title="Monitored in Arr">
                      <CheckCircle size={14} />
                    </span>
                  {:else if item.monitoredStatus === 'PENDING_REVIEW'}
                    <span class="stateBadge review" title="Pending Review">
                      <AlertTriangle size={14} />
                    </span>
                  {:else if item.monitoredStatus === 'IGNORED'}
                    <span class="stateBadge ignored" title="Ignored / Blocklisted">
                      <EyeOff size={14} />
                    </span>
                  {:else}
                    <span class="stateBadge unmonitored" title="Unmonitored">
                      <XCircle size={14} />
                    </span>
                  {/if}
                </div>

                <!-- Bottom Status Banner Overlay -->
                <div class="statusOverlay">
                  <Label kind={getStatusKind(item.anilistStatus)} size="small">
                    {formatStatusLabel(item.anilistStatus)}
                  </Label>
                  {#if item.seasons}
                    <span class="seasonPill">S{item.seasons}</span>
                  {/if}
                </div>
              </div>

              <!-- Card Details -->
              <div class="cardInfo">
                <div class="cardTitle" title={item.titleRomaji}>
                  {item.titleRomaji}
                </div>
                {#if item.titleEnglish && item.titleEnglish !== item.titleRomaji}
                  <div class="cardSubtitle" title={item.titleEnglish}>
                    {item.titleEnglish}
                  </div>
                {/if}

                <div class="cardMeta">
                  <span>{item.format || item.mediaType}</span>
                  {#if item.releaseYear}
                    <span class="metaDot">•</span>
                    <span>{item.releaseYear}</span>
                  {/if}
                  {#if item.tvdbId}
                    <span class="metaDot">•</span>
                    <span class="idTag">TVDB {item.tvdbId}</span>
                  {:else if item.tmdbId}
                    <span class="metaDot">•</span>
                    <span class="idTag">TMDB {item.tmdbId}</span>
                  {/if}
                </div>

                <!-- Quick Action Buttons -->
                <div class="cardActions">
                  <a
                    href="https://anilist.co/anime/{item.anilistId}"
                    target="_blank"
                    rel="noreferrer"
                    class="actionBtn"
                    title="View on AniList"
                  >
                    <ExternalLink size={13} />
                    <span>AniList</span>
                  </a>

                  <button
                    type="button"
                    class="actionBtn"
                    on:click={() => openOverrideModal(item)}
                    title="Configure custom override"
                  >
                    <SlidersHorizontal size={13} />
                    <span>Override</span>
                  </button>
                </div>
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <!-- TABLE VIEW (Sonarr Style) -->
        <Table>
          <thead>
            <tr>
              <th style="width: 50px;"></th>
              <th>Title</th>
              <th>Format</th>
              <th>Watch Status</th>
              <th>Destination</th>
              <th>Mapped ID</th>
              <th>State</th>
              <th>Seasons</th>
              <th style="text-align: right;">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each libraryItems as item (item.anilistId)}
              <tr>
                <!-- Poster Thumbnail -->
                <td>
                  <div class="tablePosterWrapper">
                    {#if item.posterUrl && !failedImages.has(item.anilistId)}
                      <img
                        src={item.posterUrl}
                        alt=""
                        class="tableThumb"
                        on:error={() => handleImageError(item.anilistId)}
                      />
                    {:else}
                      <div class="tableThumb tableThumbFallback">
                        <Tv size={14} />
                      </div>
                    {/if}
                  </div>
                </td>

                <!-- Title -->
                <td>
                  <div class="tableTitleRomaji">{item.titleRomaji}</div>
                  {#if item.titleEnglish && item.titleEnglish !== item.titleRomaji}
                    <div class="tableTitleEnglish">{item.titleEnglish}</div>
                  {/if}
                </td>

                <!-- Format & Year -->
                <td>
                  <span>{item.format || item.mediaType}</span>
                  {#if item.releaseYear}
                    <span class="muted"> ({item.releaseYear})</span>
                  {/if}
                </td>

                <!-- Watch Status -->
                <td>
                  <Label kind={getStatusKind(item.anilistStatus)} size="small">
                    {formatStatusLabel(item.anilistStatus)}
                  </Label>
                </td>

                <!-- Destination Service -->
                <td>
                  {#if item.targetService === 'SONARR'}
                    <span class="pill sonarrPill">Sonarr</span>
                  {:else if item.targetService === 'RADARR'}
                    <span class="pill radarrPill">Radarr</span>
                  {:else}
                    <span class="muted">-</span>
                  {/if}
                </td>

                <!-- Mapped ID -->
                <td>
                  {#if item.tvdbId}
                    <span class="codeBadge">TVDB: {item.tvdbId}</span>
                  {:else if item.tmdbId}
                    <span class="codeBadge">TMDB: {item.tmdbId}</span>
                  {:else}
                    <span class="muted">-</span>
                  {/if}
                </td>

                <!-- Monitored State -->
                <td>
                  <Label kind={getMonitoredKind(item.monitoredStatus)} size="small">
                    {item.monitoredStatus}
                  </Label>
                </td>

                <!-- Seasons -->
                <td>
                  {#if item.seasons}
                    <span>Season {item.seasons}</span>
                  {:else}
                    <span class="muted">-</span>
                  {/if}
                </td>

                <!-- Actions -->
                <td style="text-align: right;">
                  <div class="tableActions">
                    <a
                      href="https://anilist.co/anime/{item.anilistId}"
                      target="_blank"
                      rel="noreferrer"
                      class="tableActionLink"
                      title="View on AniList"
                    >
                      <ExternalLink size={14} />
                    </a>
                    <button
                      type="button"
                      class="tableActionBtn"
                      title="Edit Override"
                      on:click={() => openOverrideModal(item)}
                    >
                      <SlidersHorizontal size={14} />
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

  <!-- Override Edit Modal -->
  <Modal
    isOpen={isOverrideModalOpen}
    title="Custom Mapping Override"
    size="medium"
    on:close={() => (isOverrideModalOpen = false)}
  >
    <div slot="body">
      {#if overrideItem}
        <div class="overrideForm">
          <div class="formRow">
            <span class="labelSpan">Anime</span>
            <div class="valDisplay">
              <strong>{overrideItem.titleRomaji}</strong>
              <span class="muted"> (AniList ID: {overrideItem.anilistId})</span>
            </div>
          </div>

          <div class="formRow">
            <label for="overrideMediaType" class="labelSpan">Media Type</label>
            <select id="overrideMediaType" bind:value={overrideMediaType} class="formInput">
              <option value="SERIES">Series (Sonarr)</option>
              <option value="MOVIE">Movie (Radarr)</option>
            </select>
          </div>

          {#if overrideMediaType === 'SERIES'}
            <div class="formRow">
              <label for="overrideTvdbId" class="labelSpan">TheTVDB ID</label>
              <input
                id="overrideTvdbId"
                type="number"
                bind:value={overrideTvdbId}
                placeholder="e.g. 267440"
                class="formInput"
              />
            </div>
            <div class="formRow">
              <label for="overrideSeasons" class="labelSpan">Seasons</label>
              <input
                id="overrideSeasons"
                type="text"
                bind:value={overrideSeasons}
                placeholder="e.g. 1, 2 or 1-4"
                class="formInput"
              />
            </div>
          {:else}
            <div class="formRow">
              <label for="overrideTmdbId" class="labelSpan">TMDB ID</label>
              <input
                id="overrideTmdbId"
                type="number"
                bind:value={overrideTmdbId}
                placeholder="e.g. 129"
                class="formInput"
              />
            </div>
          {/if}

          <div class="formRow">
            <label for="overrideTitle" class="labelSpan">Title Override</label>
            <input
              id="overrideTitle"
              type="text"
              bind:value={overrideTitle}
              placeholder="Optional title in Sonarr / Radarr"
              class="formInput"
            />
          </div>
        </div>
      {/if}
    </div>
    <div slot="footer">
      <Button kind="default" on:click={() => (isOverrideModalOpen = false)}>
        Cancel
      </Button>
      <Button kind="primary" disabled={overrideSaving} on:click={saveOverride}>
        {overrideSaving ? 'Saving...' : 'Save Override'}
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

  /* Toolbar items */
  .searchWrapper {
    position: relative;
    display: flex;
    align-items: center;
    align-self: center;
    margin-left: 12px;
  }

  :global(.searchIcon) {
    position: absolute;
    left: 8px;
    color: var(--disabledColor);
    pointer-events: none;
  }

  .searchInput {
    background-color: var(--inputBackgroundColor);
    border: 1px solid var(--borderColor);
    border-radius: 4px;
    color: var(--textColor);
    height: 30px;
    box-sizing: border-box;
    padding: 0 24px 0 28px;
    font-size: var(--smallFontSize);
    width: 150px;
  }

  .searchInput:focus {
    outline: none;
    border-color: var(--primaryBorderColor);
  }

  .clearBtn {
    position: absolute;
    right: 6px;
    background: none;
    border: none;
    color: var(--disabledColor);
    font-size: 16px;
    cursor: pointer;
    line-height: 1;
    padding: 0;
  }

  .toolbarSelect {
    align-self: center;
    height: 30px;
    box-sizing: border-box;
    background-color: var(--inputBackgroundColor);
    border: 1px solid var(--borderColor);
    border-radius: 4px;
    color: var(--textColor);
    padding: 0 8px;
    font-size: var(--smallFontSize);
    margin-left: 8px;
    cursor: pointer;
  }

  .toolbarSelect:focus {
    outline: none;
    border-color: var(--primaryBorderColor);
  }

  .viewToggle {
    align-self: center;
    height: 30px;
    box-sizing: border-box;
    display: flex;
    align-items: stretch;
    border: 1px solid var(--borderColor);
    border-radius: 4px;
    overflow: hidden;
    margin-left: 8px;
  }

  .toggleBtn {
    background: var(--inputBackgroundColor);
    border: none;
    color: var(--disabledColor);
    padding: 0 8px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .toggleBtn:hover {
    color: var(--textColor);
  }

  .toggleBtn.active {
    background: var(--primaryBackgroundColor);
    color: var(--white);
  }

  .statsBadge {
    align-self: center;
    font-size: var(--smallFontSize);
    color: var(--disabledColor);
    margin-left: 10px;
    white-space: nowrap;
  }

  /* Empty State */
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

  .emptyState p {
    margin-bottom: 16px;
  }

  /* POSTER GRID VIEW */
  .posterGrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(185px, 1fr));
    gap: 16px;
  }

  .card {
    background-color: var(--cardBackgroundColor);
    border: 1px solid var(--borderColor);
    border-radius: 6px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    transition: transform 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease;
  }

  .card:hover {
    transform: translateY(-2px);
    border-color: var(--primaryBorderColor);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
  }

  .posterWrapper {
    position: relative;
    width: 100%;
    aspect-ratio: 2 / 3;
    background-color: var(--pageBackground);
    overflow: hidden;
  }

  .posterImg {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .posterFallback {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--disabledColor);
  }

  .servicePill {
    position: absolute;
    top: 6px;
    left: 6px;
    z-index: 2;
  }

  .monitoredPill {
    position: absolute;
    top: 6px;
    right: 6px;
    z-index: 2;
  }

  .pill {
    font-size: 10px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 3px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .sonarrPill {
    background-color: rgba(0, 150, 214, 0.9);
    color: #fff;
  }

  .radarrPill {
    background-color: rgba(255, 165, 0, 0.9);
    color: #fff;
  }

  .unmappedPill {
    background-color: rgba(60, 60, 60, 0.85);
    color: #ccc;
  }

  .stateBadge {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background-color: rgba(20, 20, 20, 0.85);
  }

  .stateBadge.monitored {
    color: var(--successColor);
  }

  .stateBadge.unmonitored {
    color: var(--disabledColor);
  }

  .stateBadge.review {
    color: var(--warningColor);
  }

  .stateBadge.ignored {
    color: var(--dangerColor);
  }

  .statusOverlay {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    padding: 6px 8px;
    background: linear-gradient(to top, rgba(0, 0, 0, 0.85) 0%, rgba(0, 0, 0, 0) 100%);
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
  }

  .seasonPill {
    background-color: rgba(0, 0, 0, 0.7);
    color: var(--textColor);
    font-size: 10px;
    font-weight: bold;
    padding: 1px 4px;
    border-radius: 2px;
  }

  .cardInfo {
    padding: 10px;
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
  }

  .cardTitle {
    font-size: 13px;
    font-weight: 600;
    color: var(--textColor);
    line-height: 1.3;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    margin-bottom: 2px;
  }

  .cardSubtitle {
    font-size: 11px;
    color: var(--disabledColor);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin-bottom: 6px;
  }

  .cardMeta {
    font-size: 11px;
    color: var(--disabledColor);
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 10px;
  }

  .metaDot {
    color: var(--borderColor);
  }

  .idTag {
    color: var(--textColor);
  }

  .cardActions {
    margin-top: auto;
    display: flex;
    gap: 6px;
    border-top: 1px solid var(--borderColor);
    padding-top: 8px;
  }

  .actionBtn {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    font-size: 11px;
    color: var(--disabledColor);
    background: var(--inputBackgroundColor);
    border: 1px solid var(--borderColor);
    border-radius: 3px;
    padding: 3px 6px;
    text-decoration: none;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }

  .actionBtn:hover {
    color: var(--textColor);
    border-color: var(--primaryBorderColor);
  }

  /* TABLE VIEW */
  .tablePosterWrapper {
    width: 36px;
    height: 52px;
    border-radius: 3px;
    overflow: hidden;
    background-color: var(--pageBackground);
  }

  .tableThumb {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .tableThumbFallback {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--disabledColor);
  }

  .tableTitleRomaji {
    font-weight: 600;
    color: var(--textColor);
  }

  .tableTitleEnglish {
    font-size: var(--smallFontSize);
    color: var(--disabledColor);
  }

  .codeBadge {
    font-family: var(--monoSpaceFontFamily);
    font-size: 11px;
    color: var(--disabledColor);
  }

  .muted {
    color: var(--disabledColor);
  }

  .tableActions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .tableActionLink,
  .tableActionBtn {
    background: none;
    border: none;
    color: var(--disabledColor);
    cursor: pointer;
    padding: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 3px;
    text-decoration: none;
  }

  .tableActionLink:hover,
  .tableActionBtn:hover {
    color: var(--textColor);
    background-color: var(--hoverBackground);
  }

  /* Override Modal Form */
  .overrideForm {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .formRow {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .labelSpan {
    font-size: var(--smallFontSize);
    color: var(--disabledColor);
  }

  .valDisplay {
    color: var(--textColor);
  }

  .formInput {
    background-color: var(--inputBackgroundColor);
    border: 1px solid var(--borderColor);
    border-radius: 4px;
    color: var(--textColor);
    padding: 6px 10px;
    font-size: var(--defaultFontSize);
  }

  .formInput:focus {
    outline: none;
    border-color: var(--primaryBorderColor);
  }
</style>
