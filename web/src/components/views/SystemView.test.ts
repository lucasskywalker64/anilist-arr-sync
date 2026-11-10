// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import SystemView from './SystemView.svelte';

let component: SystemView | undefined;
afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  vi.unstubAllGlobals();
});

it('displays API backup byte counts with IEC units and creation dates in the user locale', async () => {
  const cases = [
    [0, '0 B'],
    [1023, '1023 B'],
    [1024, '1 KiB'],
    [467968, '457 KiB'],
    [1048576, '1 MiB'],
    [1073741824, '1 GiB'],
    [1264, '1.23 KiB'],
    [11264, '11 KiB'],
    [1229, '1.2 KiB'],
  ] as const;
  const createdAt = '2026-11-10T14:30:00Z';
  const backups = cases.map(([size_bytes], index) => ({
    filename: `backup-${index}.zip`, size_bytes, created_at: createdAt,
  }));
  vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response(JSON.stringify(backups), {
    headers: { 'content-type': 'application/json' },
  })));

  component = new SystemView({ target: document.body, props: { subTab: 'backup' } });
  await vi.waitFor(() => expect(document.querySelectorAll('tbody tr')).toHaveLength(cases.length));

  const rows = [...document.querySelectorAll('tbody tr')];
  for (const [index, [, expectedSize]] of cases.entries()) {
    const cells = rows[index].querySelectorAll('td');
    expect(cells[0].textContent).toBe(backups[index].filename);
    expect(cells[1].textContent).toBe(expectedSize);
    expect(cells[2].textContent).toBe(new Date(createdAt).toLocaleString());
  }
});
