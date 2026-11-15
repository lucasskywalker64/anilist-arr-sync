// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { api } from '../../lib/api';
import OverridesManager from './OverridesManager.svelte';
import type { OverrideItem } from '../../lib/types';

vi.mock('../../lib/api', () => ({ api: { getOverrides: vi.fn(), createOverride: vi.fn() } }));
let component: OverridesManager | undefined;
afterEach(() => { component?.$destroy(); document.body.replaceChildren(); vi.resetAllMocks(); });

it.each(['0', '1'])('preserves season %s episode selections when editing only the title', async (seasons) => {
  const item = { id:1, anilistId:100, mediaType:'SERIES', tvdbId:123, seasons, episodeNumbers:[13,14], titleOverride:'Old' } as OverrideItem;
  vi.mocked(api.getOverrides).mockResolvedValue([item]);
  vi.mocked(api.createOverride).mockResolvedValue({status:'ok'});
  component = new OverridesManager({target:document.body});
  await vi.waitFor(() => expect(document.querySelector('[title="Edit override"]')).not.toBeNull());
  (document.querySelector('[title="Edit override"]') as HTMLButtonElement).click();
  await tick();
  const inputs = document.querySelectorAll<HTMLInputElement>('input');
  const title = [...inputs].find(input => input.value === 'Old')!;
  title.value = 'New'; title.dispatchEvent(new Event('input', {bubbles:true}));
  await tick();
  [...document.querySelectorAll('button')].find(button => button.textContent?.includes('Save Override'))!.click();
  await vi.waitFor(() => expect(api.createOverride).toHaveBeenCalledWith(expect.objectContaining({titleOverride:'New', episodeNumbers:[13,14]})));
});
