import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, setApiKey, setCsrfToken, waitForRestart } from './api';

afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); });

describe('lifecycle API', () => {
  it('uses session CSRF without requiring an API key', async () => {
    setApiKey('');
    setCsrfToken('csrf');
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ restarting: true, instanceId: 'old', reconnectUrl: 'http://localhost/' }), { headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);
    const result = await api.restart();
    expect(result.instanceId).toBe('old');
    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/system/restart');
    expect(fetchMock.mock.calls[0][1].headers['X-CSRF-Token']).toBe('csrf');
    expect(fetchMock.mock.calls[0][1].headers['X-Api-Key']).toBeUndefined();
  });

  it('waits past old health, downtime, and non-health responses for a new boot', async () => {
    vi.useFakeTimers();
    const health = (id: string) => new Response(JSON.stringify({ status: 'ok', instanceId: id }), { headers: { 'Content-Type': 'application/json' } });
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(health('old'))
      .mockRejectedValueOnce(new Error('connection refused'))
      .mockResolvedValueOnce(new Response('<html>proxy error</html>'))
      .mockResolvedValueOnce(health('new'));
    vi.stubGlobal('fetch', fetchMock);
    let finished = false;
    const waiting = waitForRestart('old', 'http://localhost:9123/new/').then(() => { finished = true; });
    await vi.advanceTimersByTimeAsync(2000);
    expect(finished).toBe(false);
    await vi.advanceTimersByTimeAsync(2000);
    await waiting;
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost:9123/new/api/v1/health');
    expect(finished).toBe(true);
  });
});
