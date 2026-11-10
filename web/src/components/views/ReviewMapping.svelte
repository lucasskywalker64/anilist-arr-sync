<script lang="ts">
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { Search, RefreshCw, Check, Ban, X } from 'lucide-svelte';
  import Button from '../common/Button.svelte';
  import { api } from '../../lib/api';
  import type { Candidate, ReviewItem, ReviewPreparation } from '../../lib/types';

  export let item: ReviewItem;
  const dispatch = createEventDispatcher<{ resolved: string; locked: boolean }>();
  let mediaType = item.mediaType || 'SERIES';
  let targetId = String((mediaType === 'SERIES' ? item.candidates?.[0]?.tvdbId : item.candidates?.[0]?.tmdbId) ?? '');
  let preparation: ReviewPreparation | null = null;
  let movie: Awaited<ReturnType<typeof api.lookupReviewMovie>> | null = null;
  let season = '';
  let scope = /\b(part|cour)\s+\d+/i.test(item.titleRomaji) ? 'episodes' : 'season';
  let selected: number[] = [];
  let titleOverride = '';
  let busy = false;
  let polling = false;
  let paused = false;
  let cancelling = false;
  let destroyed = false;
  let error = '';
  let info = '';
  let timer: ReturnType<typeof setTimeout> | undefined;
  let deadline = 0;
  let pollVersion = 0;
  $: episodes = (preparation?.episodes ?? []).filter(ep => ep.seasonNumber === Number(season)).sort((a,b) => a.episodeNumber-b.episodeNumber);
  $: ready = mediaType === 'MOVIE' ? !!movie : preparation?.status === 'READY' && season !== '' && (season !== '0' && scope === 'season' || selected.length > 0);
  $: dispatch('locked', busy || !!preparation);

  export function selectCandidate(candidate: Candidate) {
    if (preparation || busy) return;
    targetId = String((mediaType === 'SERIES' ? candidate.tvdbId : candidate.tmdbId) ?? '');
    clearMatch();
    titleOverride = candidate.title;
  }

  function clearMatch() { movie = null; selected = []; season = ''; error = ''; }
  function stopPolling() {
    if (timer) clearTimeout(timer);
    pollVersion++;
    polling = false;
  }
  function schedulePoll(delay: number) {
    const version = pollVersion;
    timer = setTimeout(async () => {
      try {
        const result = await api.getReviewPreparation(item.id);
        if (version === pollVersion) accept(result);
      } catch (e: any) {
        if (version === pollVersion) {
          error = e.message;
          paused = true;
          stopPolling();
        }
      }
    }, delay);
  }
  function accept(result: ReviewPreparation) {
    if (destroyed) return;
    preparation = result;
    if (result.status === 'CANCELLED' || result.status === 'RETAINED') {
      info = result.message || 'Preparation cancelled.';
      preparation = null; cancelling = false; paused = false; stopPolling(); clearMatch(); return;
    }
    if (result.status === 'READY') {
      stopPolling(); paused = false;
      schedulePoll(60000);
      if (!season) {
        const seasons = result.series?.seasons.map(value => value.seasonNumber) ?? [];
        season = item.reason.includes('SPECIAL') && seasons.includes(0) ? '0' : seasons.includes(1) ? '1' : String(seasons[0] ?? '');
        if (season === '0') scope = 'episodes';
        const part = Number(item.titleRomaji.match(/\b(?:part|cour)\s+(\d+)/i)?.[1]);
        if (part > 0) {
          const ordered = (result.episodes ?? []).filter(ep => ep.seasonNumber === Number(season)).sort((a,b) => a.episodeNumber-b.episodeNumber);
          const groups: typeof ordered[] = [[]];
          for (const ep of ordered) { groups[groups.length-1].push(ep); if (ep.finaleType?.toLowerCase() === 'midseason') groups.push([]); }
          const parts = groups.filter(group => group.length);
          if (parts.length > 1 && parts[part-1]) selected = parts[part-1].map(ep => ep.episodeNumber);
        }
      }
      return;
    }
    if (result.status === 'ERROR') { stopPolling(); error = result.message || 'Sonarr could not prepare episodes.'; paused = true; return; }
    pollLater();
  }
  function pollLater() {
    stopPolling();
    if (destroyed) return;
    if (Date.now() >= deadline) { paused = true; return; }
    polling = true;
    schedulePoll(1500);
  }
  async function lookup() {
    busy = true; error = ''; info = ''; paused = false; deadline = Date.now()+60000;
    try {
      if (mediaType === 'MOVIE') movie = await api.lookupReviewMovie(Number(targetId));
      else {
        // Retain cancellation access even when the HTTP response leaves add ownership uncertain.
        preparation = { status: 'WAITING', created: false };
        accept(await api.prepareReview(item.id,Number(targetId)));
      }
    } catch (e: any) { error = e.message || 'Lookup failed'; paused = !!preparation; }
    finally { busy = false; }
  }
  async function cancelPreparation() {
    busy = true; error = ''; stopPolling(); cancelling = true; deadline = Date.now()+60000;
    try { accept(await api.cancelReviewPreparation(item.id)); }
    catch (e: any) { error = e.message || 'Cleanup failed. The preparation will be retried.'; paused = true; }
    finally { busy = false; }
  }
  function continueWaiting() { error = ''; paused = false; deadline = Date.now()+60000; pollLater(); }
  function chooseSeason() { selected = []; if (season === '0') scope = 'episodes'; }
  function toggleEpisode(number: number) { selected = selected.includes(number) ? selected.filter(value => value !== number) : [...selected,number].sort((a,b) => a-b); }
  async function confirm() {
    if (!ready) return;
    busy = true; error = ''; stopPolling();
    try {
      await api.resolveReview(item.id,{
        mediaType,
        preparationRequired: mediaType === 'SERIES',
        tvdbId: mediaType === 'SERIES' ? Number(targetId) : null,
        tmdbId: mediaType === 'MOVIE' ? Number(targetId) : null,
        seasons: mediaType === 'SERIES' ? season : '1',
        episodeNumbers: mediaType === 'SERIES' && (scope === 'episodes' || season === '0') ? selected : [],
        titleOverride: titleOverride.trim() || undefined,
      });
      preparation = null; stopPolling(); dispatch('resolved',`Resolved "${item.titleRomaji}" and saved its mapping.`);
    } catch (e: any) { error = e.message || 'Confirmation failed'; if (preparation && !destroyed) schedulePoll(60000); }
    finally { busy = false; }
  }
  async function ignore() {
    busy = true; error = '';
    try { await api.ignoreReview(item.id); dispatch('resolved',`Ignored "${item.titleRomaji}".`); }
    catch (e: any) { error = e.message; }
    finally { busy = false; }
  }
  onDestroy(() => {
    destroyed = true; stopPolling();
    if (preparation) void api.cancelReviewPreparation(item.id).catch(() => {});
  });
</script>

<div class="mapping">
  <div class="lookupRow">
    <label>Destination<select bind:value={mediaType} on:change={() => { targetId = ''; clearMatch(); }} disabled={busy || !!preparation}><option value="SERIES">Series · Sonarr</option><option value="MOVIE">Movie · Radarr</option></select></label>
    <label>{mediaType === 'SERIES' ? 'TVDB series ID' : 'TMDB movie ID'}<input type="text" inputmode="numeric" bind:value={targetId} on:input={clearMatch} disabled={busy || !!preparation} /></label>
    <Button disabled={busy || !!preparation || !/^[1-9]\d*$/.test(targetId)} on:click={lookup}><Search size={16} />{mediaType === 'SERIES' ? 'Load episodes' : 'Lookup'}</Button>
  </div>
  {#if mediaType === 'SERIES' && !preparation}<p class="hint">Loading may add an unmonitored series. Cancel removes only a series created for this review, preserving files.</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if info}<p class="hint" role="status">{info}</p>{/if}
  {#if movie}<div class="match"><strong>{movie.title}</strong>{#if movie.year} ({movie.year}){/if}<p>{movie.overview ?? ''}</p></div>{/if}
  {#if preparation}
    {#if preparation.status === 'READY' && !cancelling}
      <div class="match"><strong>{preparation.series?.title}</strong>{#if preparation.series?.year} ({preparation.series.year}){/if}<p class="hint">{preparation.created ? 'Added unmonitored for this review' : 'Already in Sonarr'}</p></div>
      <div class="scopeRow">
        <label>Season<select bind:value={season} on:change={chooseSeason}><option value="">Choose a season</option>{#each preparation.series?.seasons ?? [] as value}<option value={String(value.seasonNumber)}>{value.seasonNumber === 0 ? 'Specials (Season 0)' : `Season ${value.seasonNumber}`}</option>{/each}</select></label>
        {#if season !== '' && season !== '0'}<label>Scope<select bind:value={scope} on:change={() => selected = []}><option value="season">Whole season</option><option value="episodes">Selected episodes / part</option></select></label>{/if}
      </div>
      {#if season !== '' && (scope === 'episodes' || season === '0')}
        <fieldset><legend>Select episodes for this AniList entry</legend>{#each episodes as episode}<label class="episode"><input type="checkbox" checked={selected.includes(episode.episodeNumber)} on:change={() => toggleEpisode(episode.episodeNumber)} /> <span>Episode {episode.episodeNumber}: {episode.title}</span>{#if selected.includes(episode.episodeNumber)}<span class="hint">AniList {selected.indexOf(episode.episodeNumber)+1}</span>{/if}</label>{/each}</fieldset>
        <p class="hint">{selected.length ? `Selected episodes: ${selected.join(', ')}. Other episodes stay outside this mapping.` : 'Select at least one episode. No part boundary is assumed.'}</p>
      {/if}
    {:else}
      <p class="waiting" aria-live="polite">{#if polling || busy}<RefreshCw size={16} class="spinning" />{/if}{cancelling ? 'Removing the temporary series after its initial refresh…' : paused ? 'Preparation is taking longer or needs attention.' : 'Loading Sonarr episode metadata…'}</p>
    {/if}
    <div class="actions"><Button disabled={busy} on:click={cancelPreparation}><X size={16} />Cancel preparation</Button>{#if paused}<Button disabled={busy} on:click={continueWaiting}>Check again</Button>{/if}</div>
  {/if}
  {#if ready}<label>Title override (optional)<input type="text" bind:value={titleOverride} disabled={busy} /></label>{/if}
  <div class="actions"><Button kind="success" disabled={busy || !ready || cancelling} on:click={confirm}><Check size={16} />Confirm mapping</Button><Button kind="danger" disabled={busy || !!preparation} on:click={ignore}><Ban size={16} />Ignore title</Button></div>
  <p class="hint">Monitoring and searches follow your configured sync mode after confirmation.</p>
</div>

<style>
  .lookupRow,.scopeRow,.actions { display:flex; flex-wrap:wrap; gap:12px; align-items:end; margin:12px 0; }
  label { display:flex; flex-direction:column; gap:6px; }
  select,input[type="text"] { min-height:36px; max-width:100%; background:var(--inputBackgroundColor); color:var(--textColor); border:1px solid var(--borderColor); border-radius:4px; padding:6px 9px; }
  .match { border-top:1px solid var(--borderColor); padding-top:16px; margin-top:16px; }
  .hint { color:var(--helpTextColor); font-size:12px; }
  .error { color:var(--dangerColor); }
  .waiting { display:flex; gap:8px; align-items:center; }
  fieldset { border:1px solid var(--borderColor); margin:12px 0; }
  .episode { flex-direction:row; align-items:center; gap:10px; padding:7px 0; }
  .episode .hint { margin-left:auto; }
  @media(max-width:600px) { .lookupRow>label,.scopeRow>label{flex:1 1 100%;min-width:0;} .episode{flex-wrap:wrap;} }
</style>
