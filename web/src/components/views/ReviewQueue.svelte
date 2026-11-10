<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { RefreshCw, ExternalLink, Film, Tv } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { ReviewItem, Candidate } from '../../lib/types';
  import ReviewMapping from './ReviewMapping.svelte';

  export let reviewItems: ReviewItem[] = [];

  const dispatch = createEventDispatcher<{ refresh: void }>();

  let loading = false;
  let errorMsg = '';
  let successMsg = '';

  const mappingForms: Record<number, ReviewMapping> = {};
  let lockedItems = new Set<number>();
  let failedImages = new Set<number>();

  function handleImageError(id: number) {
    failedImages.add(id);
    failedImages = new Set(failedImages);
  }

  function selectCandidate(itemId: number, candidate: Candidate) {
    mappingForms[itemId]?.selectCandidate(candidate);
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      reviewItems = await api.getReviewQueue();
      dispatch('refresh');
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load review queue';
    } finally {
      loading = false;
    }
  }

  function markLocked(id: number, locked: boolean) {
    if (locked) lockedItems.add(id); else lockedItems.delete(id);
    lockedItems = new Set(lockedItems);
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
                          disabled={lockedItems.has(item.id)}
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
                  <ReviewMapping {item} bind:this={mappingForms[item.id]} on:locked={event => markLocked(item.id,event.detail)} on:resolved={event => { successMsg = event.detail; loadData(); }} />
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

  @media (max-width: 768px) {
    .reviewCard {
      flex-direction: column;
    }
    .posterContainer {
      margin-bottom: 15px;
    }
  }
</style>
