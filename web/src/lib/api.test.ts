import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api, setApiKey, getApiKey } from './api';

describe('API Client', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    setApiKey('');
  });

  it('manages API key storage', () => {
    expect(getApiKey()).toBe('');
    setApiKey('secret-token-123');
    expect(getApiKey()).toBe('secret-token-123');
  });

  it('automatically attaches X-Api-Key header on state-mutating POST requests', async () => {
    setApiKey('my-api-key');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'started' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await api.triggerSync();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/sync/start');
    expect(options.method).toBe('POST');
    expect(options.headers['X-Api-Key']).toBe('my-api-key');
  });

  it('automatically attaches X-Api-Key header on PUT requests', async () => {
    setApiKey('put-token');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'ok' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await api.updateConfig({ instanceName: 'My Server' });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/config');
    expect(options.method).toBe('PUT');
    expect(options.headers['X-Api-Key']).toBe('put-token');
    expect(JSON.parse(options.body)).toEqual({ instanceName: 'My Server' });
  });

  it('automatically attaches X-Api-Key header on DELETE requests', async () => {
    setApiKey('delete-token');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'deleted' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await api.deleteOverride(42);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/overrides/42');
    expect(options.method).toBe('DELETE');
    expect(options.headers['X-Api-Key']).toBe('delete-token');
  });

  it('calls resolve review with TVDB ID and attaches X-Api-Key', async () => {
    setApiKey('resolve-token');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'RESOLVED', id: 10 }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await api.resolveReview(10, {
      mediaType: 'SERIES',
      tvdbId: 80348,
      seasons: '1',
      titleOverride: 'Attack on Titan',
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/review/10/resolve');
    expect(options.method).toBe('POST');
    expect(options.headers['X-Api-Key']).toBe('resolve-token');
    expect(JSON.parse(options.body)).toEqual({
      mediaType: 'SERIES',
      tvdbId: 80348,
      seasons: '1',
      titleOverride: 'Attack on Titan',
    });
  });

  it('calls ignore review with reason and attaches X-Api-Key', async () => {
    setApiKey('ignore-token');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'IGNORED', id: 5 }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await api.ignoreReview(5, 'Not anime');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/review/5/ignore');
    expect(options.method).toBe('POST');
    expect(options.headers['X-Api-Key']).toBe('ignore-token');
    expect(JSON.parse(options.body)).toEqual({ reason: 'Not anime' });
  });

  it('calls staged apply and reject with IDs', async () => {
    setApiKey('staged-token');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'ok' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await api.applyStaged([1, 2, 3]);
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/v1/staged/apply',
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ 'X-Api-Key': 'staged-token' }),
        body: JSON.stringify({ ids: [1, 2, 3] }),
      })
    );

    await api.rejectStaged([4]);
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/v1/staged/reject',
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ 'X-Api-Key': 'staged-token' }),
        body: JSON.stringify({ ids: [4] }),
      })
    );
  });
});
