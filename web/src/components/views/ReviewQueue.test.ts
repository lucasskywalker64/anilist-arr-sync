// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { api } from '../../lib/api';
import ReviewQueue from './ReviewQueue.svelte';
import type { ReviewItem, ReviewPreparation } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  api: { getReviewQueue: vi.fn(), prepareReview: vi.fn(), getReviewPreparation: vi.fn(), cancelReviewPreparation: vi.fn().mockResolvedValue({status:'CANCELLED'}), lookupReviewMovie: vi.fn(), resolveReview: vi.fn() },
}));

let component: ReviewQueue | undefined;
afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  vi.resetAllMocks();
});

const button = (label: string) => [...document.querySelectorAll('button')].find(el => el.textContent?.includes(label))!;
const item = { id: 1, anilistId: 204431, mediaType: 'SERIES', reason:'SPECIAL_WITHOUT_MAPPING', titleRomaji: 'Narumi no Heijitsu', candidates: [{ tvdbId: 423075, title: 'Kaiju No. 8' }] } as ReviewItem;
const prepared: ReviewPreparation = {status:'READY',created:true,series:{id:10,tvdbId:423075,title:'Kaiju No. 8',seasons:[{seasonNumber:0},{seasonNumber:1}]},episodes:[1,4,5,6,7].map(episodeNumber=>({id:100+episodeNumber,seasonNumber:0,episodeNumber,title:`Special ${episodeNumber}`}))};

it('waits for preparation then saves only selected Specials episodes', async () => {
  vi.mocked(api.getReviewQueue).mockResolvedValueOnce([item]).mockResolvedValue([]);
  vi.mocked(api.prepareReview).mockResolvedValue({status:'WAITING',created:true});
  vi.mocked(api.getReviewPreparation).mockResolvedValue(prepared);
  component = new ReviewQueue({ target: document.body });
  await vi.waitFor(() => expect(button('Load episodes')).toBeDefined());
  expect(document.querySelectorAll('input[inputmode="numeric"]')).toHaveLength(1);
  button('Load episodes').click();
  await vi.waitFor(() => expect(document.body.textContent).toContain('Loading Sonarr episode metadata'));
  expect(button('Confirm mapping').disabled).toBe(true);
  await vi.waitFor(() => expect(document.querySelectorAll('fieldset input')).toHaveLength(5),{timeout:3000});
  expect(api.prepareReview).toHaveBeenCalledWith(1,423075);
  expect(button('Confirm mapping').disabled).toBe(true);
  expect(document.querySelector('fieldset')?.textContent).toContain('Episode 4: Special 4');
  for (const input of [...document.querySelectorAll<HTMLInputElement>('fieldset input')].slice(1)) input.click();
  await tick();
  expect(button('Confirm mapping').disabled).toBe(false);
  vi.mocked(api.resolveReview).mockResolvedValue({ status: 'RESOLVED' });
  button('Confirm mapping').click();
  await vi.waitFor(() => expect(api.resolveReview).toHaveBeenCalledWith(1, expect.objectContaining({
    tvdbId: 423075, tmdbId:null, preparationRequired:true, seasons: '0', episodeNumbers: [4, 5, 6, 7],
  })));
  expect(api.cancelReviewPreparation).not.toHaveBeenCalled();
});

it('cancels preparation before allowing a target change',async()=>{
  vi.mocked(api.getReviewQueue).mockResolvedValue([item]);
  vi.mocked(api.prepareReview).mockResolvedValue(prepared);
  vi.mocked(api.cancelReviewPreparation).mockResolvedValue({status:'CANCELLED',created:true});
  component=new ReviewQueue({target:document.body});
  await vi.waitFor(()=>expect(button('Load episodes')).toBeDefined());button('Load episodes').click();
  await vi.waitFor(()=>expect(button('Cancel preparation')).toBeDefined());
  expect(document.querySelector<HTMLInputElement>('input[inputmode="numeric"]')!.disabled).toBe(true);
  button('Cancel preparation').click();
  await vi.waitFor(()=>expect(button('Load episodes').disabled).toBe(false));
  expect(api.cancelReviewPreparation).toHaveBeenCalledWith(1);
  const destination=document.querySelector<HTMLSelectElement>('select')!;destination.value='MOVIE';destination.dispatchEvent(new Event('change',{bubbles:true}));
  await tick();expect(document.querySelector<HTMLInputElement>('input[inputmode="numeric"]')!.value).toBe('');
  expect(document.body.textContent).toContain('TMDB movie ID');
});

it('ignores an in-flight poll that finishes after cancellation', async () => {
  vi.mocked(api.getReviewQueue).mockResolvedValue([item]);
  vi.mocked(api.prepareReview).mockResolvedValue({status:'WAITING',created:true});
  let finishPoll!: (value: ReviewPreparation) => void;
  vi.mocked(api.getReviewPreparation).mockReturnValue(new Promise(resolve => { finishPoll = resolve; }));
  vi.mocked(api.cancelReviewPreparation).mockResolvedValue({status:'CANCELLED',created:true});
  component = new ReviewQueue({target:document.body});
  await vi.waitFor(() => expect(button('Load episodes')).toBeDefined());
  button('Load episodes').click();
  await vi.waitFor(() => expect(api.getReviewPreparation).toHaveBeenCalled(), {timeout:3000});
  button('Cancel preparation').click();
  await vi.waitFor(() => expect(button('Load episodes').disabled).toBe(false));
  finishPoll(prepared);
  await tick();
  expect(button('Load episodes').disabled).toBe(false);
  expect(document.querySelector('fieldset')).toBeNull();
  expect(button('Confirm mapping').disabled).toBe(true);
});

it('looks up an unadded movie and sends only its TMDB ID',async()=>{
  vi.mocked(api.getReviewQueue).mockResolvedValue([{...item,mediaType:'MOVIE',reason:'UNMAPPED',candidates:[{tmdbId:372058,title:'Your Name.'}]}]);
  vi.mocked(api.lookupReviewMovie).mockResolvedValue({tmdbId:372058,title:'Your Name.',year:2016});
  vi.mocked(api.resolveReview).mockResolvedValue({status:'RESOLVED'});
  component=new ReviewQueue({target:document.body});await vi.waitFor(()=>expect(button('Lookup')).toBeDefined());button('Lookup').click();
  await vi.waitFor(()=>expect(button('Confirm mapping').disabled).toBe(false));button('Confirm mapping').click();
  await vi.waitFor(()=>expect(api.resolveReview).toHaveBeenCalledWith(1,expect.objectContaining({mediaType:'MOVIE',tvdbId:null,tmdbId:372058})));
  expect(document.querySelector('fieldset')).toBeNull();
});

it('requires manual episode selection for Part 2 when no boundary is available',async()=>{
  vi.mocked(api.getReviewQueue).mockResolvedValue([{...item,titleRomaji:'Anime Season 1 Part 2',reason:'UNMAPPED'}]);
  vi.mocked(api.prepareReview).mockResolvedValue({...prepared,episodes:[1,13,14].map(episodeNumber=>({id:episodeNumber,episodeNumber,seasonNumber:1,title:`Episode ${episodeNumber}`}))});
  vi.mocked(api.cancelReviewPreparation).mockResolvedValue({status:'CANCELLED',created:true});
  component=new ReviewQueue({target:document.body});await vi.waitFor(()=>expect(button('Load episodes')).toBeDefined());button('Load episodes').click();
  await vi.waitFor(()=>expect(document.querySelectorAll('fieldset input')).toHaveLength(3));
  expect(button('Confirm mapping').disabled).toBe(true);
  expect(document.querySelectorAll('fieldset input:checked')).toHaveLength(0);
  (document.querySelectorAll<HTMLInputElement>('fieldset input')[1]).click();
  await tick();
  expect(button('Confirm mapping').disabled).toBe(false);
  component.$destroy();component=undefined;
  expect(api.cancelReviewPreparation).toHaveBeenCalledWith(1);
});
