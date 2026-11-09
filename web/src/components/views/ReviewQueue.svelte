<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { RefreshCw, ExternalLink, Check, Ban, Film, Tv } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { ReviewItem, Candidate } from '../../lib/types';

  export let reviewItems: ReviewItem[] = [];

  const dispatch = createEventDispatcher<{ refresh: void }>();

  let loading = false;
  let actionLoading = false;
  let errorMsg = '';
  let successMsg = '';

  // Per-item resolution state
  interface ItemState {
    anilistId: number;
    mediaType: 'SERIES' | 'MOVIE';
    tvdbId: string;
    tmdbId: string;
    seasons: string;
    titleOverride: string;
    episodeNumbers?: number[];
    specialEpisodes?: { episodeNumber: number; title: string }[];
  }
  let formState: Record<number, ItemState> = {};
  let failedImages = new Set<number>();

  function handleImageError(id: number) {
    failedImages.add(id);
    failedImages = new Set(failedImages);
  }

  $: if (reviewItems) {
    let changed = false;
    const activeIds = new Set<number>();
    for (const item of reviewItems) {
      activeIds.add(item.id);
      if (formState[item.id]?.anilistId !== item.anilistId) {
        const topCandidate = item.candidates && item.candidates.length > 0 ? item.candidates[0] : null;
        formState[item.id] = {
          anilistId: item.anilistId,
          mediaType: item.mediaType || 'SERIES',
          tvdbId: topCandidate?.tvdbId ? String(topCandidate.tvdbId) : '',
          tmdbId: topCandidate?.tmdbId ? String(topCandidate.tmdbId) : '',
          seasons: '1',
          titleOverride: '',
        };
        changed = true;
      }
    }
    for (const key of Object.keys(formState)) {
      const id = Number(key);
      if (!activeIds.has(id)) {
        delete formState[id];
        changed = true;
      }
    }
    if (changed) {
      formState = { ...formState };
    }
  }

  function initFormState(items: ReviewItem[]) {
    const next: Record<number, ItemState> = {};
    for (const item of items) {
      if (formState[item.id]?.anilistId === item.anilistId) {
        next[item.id] = formState[item.id];
      } else {
        const topCandidate = item.candidates && item.candidates.length > 0 ? item.candidates[0] : null;
        next[item.id] = {
          anilistId: item.anilistId,
          mediaType: item.mediaType || 'SERIES',
          tvdbId: topCandidate?.tvdbId ? String(topCandidate.tvdbId) : '',
          tmdbId: topCandidate?.tmdbId ? String(topCandidate.tmdbId) : '',
          seasons: '1',
          titleOverride: '',
        };
      }
    }
    formState = next;
  }

  function selectCandidate(itemId: number, candidate: Candidate) {
    if (!formState[itemId]) return;
    formState[itemId].tvdbId = candidate.tvdbId ? String(candidate.tvdbId) : '';
    formState[itemId].tmdbId = candidate.tmdbId ? String(candidate.tmdbId) : '';
    formState[itemId].seasons = '1';
    clearEpisodes(itemId);
    if (candidate.title) {
      formState[itemId].titleOverride = candidate.title;
    }
    formState = { ...formState };
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      reviewItems = await api.getReviewQueue();
      initFormState(reviewItems);
      dispatch('refresh');
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load review queue';
    } finally {
      loading = false;
    }
  }

  async function handleResolve(item: ReviewItem) {
    const state = formState[item.id];
    if (!state || actionLoading) return;

    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      const tvdbStr = String(state.tvdbId ?? '').trim();
      const tmdbStr = String(state.tmdbId ?? '').trim();
      const seasonsStr = String(state.seasons ?? '').trim();
      const titleStr = String(state.titleOverride ?? '').trim();

      await api.resolveReview(item.id, {
        mediaType: state.mediaType,
        tvdbId: tvdbStr ? parseInt(tvdbStr, 10) : null,
        tmdbId: tmdbStr ? parseInt(tmdbStr, 10) : null,
        seasons: seasonsStr || '1',
        titleOverride: titleStr || undefined,
        episodeNumbers: state.mediaType === 'SERIES' && seasonsStr === '0' ? state.episodeNumbers : [],
      });

      successMsg = `Resolved "${item.titleRomaji}" and saved manual override.`;
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to resolve review item';
    } finally {
      actionLoading = false;
    }
  }

  function clearEpisodes(itemId: number) {
    formState[itemId].episodeNumbers = [];
    formState[itemId].specialEpisodes = undefined;
  }

  async function loadSpecials(itemId: number) {
    const state = formState[itemId];
    const tvdbId = Number(state.tvdbId);
    state.seasons = '0';
    clearEpisodes(itemId);
    actionLoading = true;
    errorMsg = '';
    try {
      state.specialEpisodes = await api.getSpecialEpisodes(tvdbId);
      if (state.specialEpisodes.length === 0) errorMsg = 'No Specials episodes found in Sonarr.';
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load Specials episodes';
    } finally {
      formState = { ...formState };
      actionLoading = false;
    }
  }

  function toggleEpisode(itemId: number, number: number) {
    const selected = formState[itemId].episodeNumbers ?? [];
    formState[itemId].episodeNumbers = selected.includes(number)
      ? selected.filter(value => value !== number) : [...selected, number].sort((a, b) => a - b);
  }

  async function handleIgnore(item: ReviewItem) {
    if (actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      await api.ignoreReview(item.id);
      successMsg = `Ignored "${item.titleRomaji}". It will not trigger future reviews.`;
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to ignore item';
    } finally {
      actionLoading = false;
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

      {#if reviewItems.length === 0}
        <div class="emptyState">
          <h3>Review Queue Clear</h3>
          <p>No unmapped or ambiguous anime titles currently require manual resolution.</p>
        </div>
      {:else}
        <div class="reviewList">
          {#each reviewItems as item (item.id)}
            <div class="reviewCard">
              <!-- Left Poster (170x250px) -->
              <div class="posterContainer">
                {#if item.posterUrl && !failedImages.has(item.id)}
                  <img
                    src={item.posterUrl}
                    alt={item.titleRomaji}
                    class="poster"
                    on:error={() => handleImageError(item.id)}
                  />
                {:else}
                  <div class="poster posterFallback">
                    {#if item.mediaType === 'SERIES'}
                      <Tv size={48} />
                    {:else}
                      <Film size={48} />
                    {/if}
                    <span>{item.mediaType}</span>
                  </div>
                {/if}
              </div>

              <!-- Right Content -->
              <div class="content">
                <!-- 36px font-weight 300 Title Row -->
                <div class="titleRow">
                  <span class="titleRomaji">{item.titleRomaji}</span>
                  {#if item.releaseYear}
                    <span class="year">({item.releaseYear})</span>
                  {/if}
                </div>

                {#if item.titleEnglish && item.titleEnglish !== item.titleRomaji}
                  <div class="subtitle">{item.titleEnglish}</div>
                {/if}

                <!-- Metadata Row -->
                <div class="metadataRow">
                  <Label kind={item.mediaType === 'SERIES' ? 'primary' : 'info'} size="small">
                    {item.mediaType}
                  </Label>

                  <a
                    href="https://anilist.co/anime/{item.anilistId}"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="anilistBadge"
                  >
                    <Label kind="default" size="small">
                      AniList #{item.anilistId} <ExternalLink size={10} />
                    </Label>
                  </a>

                  <Label kind="warning" size="small">
                    {item.reason || 'UNRESOLVED'}
                  </Label>
                </div>

                <!-- Candidates List -->
                {#if item.candidates && item.candidates.length > 0}
                  <div class="candidatesSection">
                    <div class="candidateSectionLabel">Suggested Matches (click to select):</div>
                    <div class="candidateList">
                      {#each item.candidates as cand}
                        <button
                          type="button"
                          class="candidatePill"
                          on:click={() => selectCandidate(item.id, cand)}
                        >
                          <span class="candidateTitle">{cand.title}</span>
                          {#if cand.year}
                            <span class="candidateYear">({cand.year})</span>
                          {/if}
                          {#if cand.tvdbId}
                            <Label kind="default" size="small">TVDB: {cand.tvdbId}</Label>
                          {/if}
                          {#if cand.tmdbId}
                            <Label kind="default" size="small">TMDB: {cand.tmdbId}</Label>
                          {/if}
                          {#if cand.similarity != null}
                            <span class="similarity">{Math.round(cand.similarity * 100)}% match</span>
                          {/if}
                        </button>
                      {/each}
                    </div>
                  </div>
                {/if}

                <!-- Resolution Controls Bar at bottom of card -->
                <div class="resolutionBar">
                  {#if formState[item.id]}
                    <div class="inputRow">
                      <div class="inputCol" style="width: 130px;">
                        <label for="media-type-{item.id}" class="inputLabel">Media Type</label>
                        <select
                          id="media-type-{item.id}"
                          class="inlineInput"
                          bind:value={formState[item.id].mediaType}
                          on:change={() => clearEpisodes(item.id)}
                          disabled={actionLoading}
                        >
                          <option value="SERIES">SERIES</option>
                          <option value="MOVIE">MOVIE</option>
                        </select>
                      </div>

                      <div class="inputCol" style="width: 130px;">
                        <label for="tvdb-id-{item.id}" class="inputLabel">TVDB ID</label>
                        <input
                          id="tvdb-id-{item.id}"
                          type="number"
                          class="inlineInput"
                          placeholder="TVDB ID"
                          bind:value={formState[item.id].tvdbId}
                          on:input={() => clearEpisodes(item.id)}
                          disabled={actionLoading}
                        />
                      </div>

                      <div class="inputCol" style="width: 130px;">
                        <label for="tmdb-id-{item.id}" class="inputLabel">TMDB ID</label>
                        <input
                          id="tmdb-id-{item.id}"
                          type="number"
                          class="inlineInput"
                          placeholder="TMDB ID"
                          bind:value={formState[item.id].tmdbId}
                          disabled={actionLoading}
                        />
                      </div>

                      <div class="inputCol" style="width: 100px;">
                        <label for="seasons-{item.id}" class="inputLabel">Seasons</label>
                        <input
                          id="seasons-{item.id}"
                          type="text"
                          class="inlineInput"
                          placeholder="1"
                          bind:value={formState[item.id].seasons}
                          on:input={() => clearEpisodes(item.id)}
                          disabled={actionLoading}
                        />
                        {#if formState[item.id].mediaType === 'SERIES'}
                          <button type="button" disabled={actionLoading || !formState[item.id].tvdbId} on:click={() => loadSpecials(item.id)}>Specials (Season 0)</button>
                        {/if}
                      </div>

                      <div class="inputCol flexGrow">
                        <label for="title-override-{item.id}" class="inputLabel">Title Override (optional)</label>
                        <input
                          id="title-override-{item.id}"
                          type="text"
                          class="inlineInput"
                          placeholder="Override search title"
                          bind:value={formState[item.id].titleOverride}
                          disabled={actionLoading}
                        />
                      </div>
                    </div>

                    {#if formState[item.id].mediaType === 'SERIES' && String(formState[item.id].seasons) === '0'}
                      <fieldset class="specialEpisodes">
                        <legend>Specials (Season 0): select at least one episode</legend>
                        {#each formState[item.id].specialEpisodes ?? [] as episode}
                          <label>
                            <input type="checkbox" checked={formState[item.id].episodeNumbers?.includes(episode.episodeNumber) ?? false} disabled={actionLoading} on:change={() => toggleEpisode(item.id, episode.episodeNumber)} />
                            Episode {episode.episodeNumber}: {episode.title}
                          </label>
                        {/each}
                      </fieldset>
                    {/if}

                    <div class="buttonRow">
                      <Button
                        kind="success"
                        size="medium"
                        disabled={actionLoading || (formState[item.id].mediaType === 'SERIES' && String(formState[item.id].seasons) === '0' && !formState[item.id].episodeNumbers?.length)}
                        on:click={() => handleResolve(item)}
                      >
                        <Check size={16} /> Resolve & Save Override
                      </Button>

                      <Button
                        kind="danger"
                        size="medium"
                        disabled={actionLoading}
                        on:click={() => handleIgnore(item)}
                      >
                        <Ban size={16} /> Ignore Title
                      </Button>
                    </div>
                  {/if}
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .specialEpisodes {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 12px 0;
    border: 1px solid var(--borderColor);
  }

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

  .reviewList {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .reviewCard {
    position: relative;
    margin: 10px 0;
    padding: 20px;
    width: 100%;
    background-color: var(--addSeriesBackgroundColor);
    transition: background-color 500ms ease, box-shadow 200ms ease-in;
    display: flex;
    border-radius: 4px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  }

  .reviewCard:hover {
    background-color: var(--pageBackground);
    box-shadow: 0 0 12px var(--black);
  }

  .posterContainer {
    flex: 0 0 170px;
    width: 170px;
    height: 250px;
    margin-right: 20px;
  }

  .poster {
    width: 170px;
    height: 250px;
    object-fit: cover;
    background-color: #333;
    border-radius: 4px;
    display: block;
  }

  .posterFallback {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--helpTextColor);
    background-color: #262626;
    border: 1px dashed var(--borderColor);
  }

  .content {
    display: flex;
    flex: 1 1 100%;
    flex-direction: column;
    overflow: hidden;
    justify-content: space-between;
  }

  .titleRow {
    font-weight: 300;
    font-size: 36px;
    line-height: 1.2;
    color: var(--white);
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 8px;
  }

  .titleRomaji {
    word-break: break-word;
  }

  .year {
    font-size: 24px;
    color: var(--disabledColor);
  }

  .subtitle {
    font-size: var(--largeFontSize);
    color: var(--helpTextColor);
    margin-top: 4px;
  }

  .metadataRow {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 10px 0;
    flex-wrap: wrap;
  }

  .anilistBadge {
    display: inline-flex;
    text-decoration: none;
  }

  .candidatesSection {
    margin: 10px 0 15px 0;
  }

  .candidateSectionLabel {
    font-size: var(--smallFontSize);
    color: var(--helpTextColor);
    margin-bottom: 6px;
  }

  .candidateList {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .candidatePill {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--borderColor);
    border-radius: 4px;
    color: var(--textColor);
    padding: 6px 10px;
    font-size: var(--smallFontSize);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    transition: background-color 150ms ease, border-color 150ms ease;
  }

  .candidatePill:hover {
    background-color: var(--toolbarMenuItemHoverBackgroundColor);
    border-color: var(--sonarrBlue);
    color: var(--white);
  }

  .candidateTitle {
    font-weight: 500;
  }

  .candidateYear {
    color: var(--disabledColor);
  }

  .similarity {
    color: var(--successColor);
    font-weight: bold;
  }

  .resolutionBar {
    margin-top: auto;
    padding-top: 15px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .inputRow {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    align-items: flex-end;
  }

  .inputCol {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .flexGrow {
    flex: 1 1 200px;
  }

  .inputLabel {
    font-size: var(--extraSmallFontSize);
    color: var(--helpTextColor);
    font-weight: bold;
    text-transform: uppercase;
  }

  .inlineInput {
    height: var(--inputHeight);
    padding: 6px 12px;
    border: 1px solid var(--inputBorderColor);
    border-radius: 4px;
    background-color: var(--inputBackgroundColor);
    color: var(--textColor);
    font-family: inherit;
    font-size: var(--defaultFontSize);
    outline: none;
    box-sizing: border-box;
    width: 100%;
  }

  .inlineInput:focus {
    border-color: var(--inputFocusBorderColor);
    box-shadow: inset 0 1px 1px var(--inputBoxShadowColor), 0 0 8px var(--inputFocusBoxShadowColor);
  }

  .buttonRow {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  @media (max-width: 768px) {
    .reviewCard {
      flex-direction: column;
    }
    .posterContainer {
      margin-bottom: 15px;
    }
  }
</style>
