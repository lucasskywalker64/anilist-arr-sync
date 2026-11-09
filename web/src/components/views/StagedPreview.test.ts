// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { api } from '../../lib/api';
import type { StagedAction } from '../../lib/types';
import StagedPreview from './StagedPreview.svelte';

vi.mock('svelte', async (importOriginal) => ({
  ...await importOriginal<typeof import('svelte')>(),
  onMount: (await vi.importActual<typeof import('svelte')>('svelte/internal')).onMount,
}));

vi.mock('../../lib/api', () => ({
  api: { getStaged: vi.fn(), applyStaged: vi.fn() },
}));

let component: StagedPreview | undefined;
afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  vi.resetAllMocks();
});

it('marks the failed row after refreshing a partially applied batch', async () => {
  const actions = [1, 2, 3].map(id => ({
    id, title: `Movie ${id}`, actionType: 'ADD_MOVIE', mediaType: 'MOVIE',
    targetService: 'RADARR', payload: '{}', status: 'PENDING', createdAt: '2026-11-09T12:00:00Z',
  })) as StagedAction[];
  vi.mocked(api.getStaged).mockResolvedValue(actions);
  component = new StagedPreview({ target: document.body });
  await vi.waitFor(() => expect(document.querySelectorAll('tbody tr')).toHaveLength(3));
  const checkboxes = [...document.querySelectorAll<HTMLElement>('tbody [role="checkbox"]')];
  expect(checkboxes).toHaveLength(3);
  for (const checkbox of checkboxes) checkbox.click();
  await tick();

  vi.mocked(api.getStaged).mockResolvedValue(actions.slice(1));
  vi.mocked(api.applyStaged).mockRejectedValue(Object.assign(new Error('Endpoint changed'), {
    applied: [1], failedId: 2,
  }));
  const apply = [...document.querySelectorAll('button')].find(button => button.textContent?.includes('Approve Selected'));
  expect(apply).toBeDefined();
  expect(apply!.disabled).toBe(false);
  apply!.click();

  await vi.waitFor(() => expect(document.querySelector('.failedRow')?.textContent).toContain('Movie 2'));
  expect(document.querySelector('.failedRow')?.textContent).toContain('Apply failed');
  expect(document.querySelectorAll('tbody tr')).toHaveLength(2);
  expect(document.querySelector('tbody')?.textContent).not.toContain('Movie 1');
  expect(document.body.textContent).toContain('Endpoint changed');
});
