// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { api } from '../../lib/api';
import ReviewQueue from './ReviewQueue.svelte';
import type { ReviewItem } from '../../lib/types';

vi.mock('svelte', async (importOriginal) => ({
  ...await importOriginal<typeof import('svelte')>(),
  onMount: (await vi.importActual<typeof import('svelte')>('svelte/internal')).onMount,
}));

vi.mock('../../lib/api', () => ({
  api: { getReviewQueue: vi.fn(), getSpecialEpisodes: vi.fn(), resolveReview: vi.fn() },
}));

let component: ReviewQueue | undefined;
afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  vi.resetAllMocks();
});

it('requires an episode selection for Specials and clears it when the TVDB ID changes', async () => {
  const item = { id: 1, anilistId: 204431, mediaType: 'SERIES', titleRomaji: 'Narumi no Heijitsu', candidates: [{ tvdbId: 423075, title: 'Kaiju No. 8' }] } as ReviewItem;
  vi.mocked(api.getReviewQueue).mockResolvedValue([item]);
  vi.mocked(api.getSpecialEpisodes).mockResolvedValue([
    { episodeNumber: 1, title: 'Outside mapping' },
    ...[4, 5, 6, 7].map(episodeNumber => ({ episodeNumber, title: `Special ${episodeNumber}` })),
  ]);
  component = new ReviewQueue({ target: document.body });
  const button = (label: string) => [...document.querySelectorAll('button')].find(el => el.textContent?.includes(label))!;
  await vi.waitFor(() => expect(button('Specials (Season 0)')).toBeDefined());
  button('Specials (Season 0)').click();
  await vi.waitFor(() => expect(document.querySelectorAll('fieldset input')).toHaveLength(5));
  expect(api.getSpecialEpisodes).toHaveBeenCalledWith(423075);
  expect(button('Resolve & Save Override').disabled).toBe(true);
  expect(document.querySelector('fieldset')?.textContent).toContain('Episode 4: Special 4');
  for (const input of [...document.querySelectorAll<HTMLInputElement>('fieldset input')].slice(1)) input.click();
  await tick();
  expect(button('Resolve & Save Override').disabled).toBe(false);
  vi.mocked(api.resolveReview).mockResolvedValue({ status: 'RESOLVED' });
  button('Resolve & Save Override').click();
  await vi.waitFor(() => expect(api.resolveReview).toHaveBeenCalledWith(1, expect.objectContaining({
    tvdbId: 423075, seasons: '0', episodeNumbers: [4, 5, 6, 7],
  })));
  await vi.waitFor(() => expect(button('Specials (Season 0)').disabled).toBe(false));
  const tvdb = document.querySelector<HTMLInputElement>('#tvdb-id-1')!;
  tvdb.value = '123';
  tvdb.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  expect(document.querySelectorAll('fieldset input')).toHaveLength(0);
  expect(button('Resolve & Save Override').disabled).toBe(true);
});
