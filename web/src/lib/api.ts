import type {
  ReviewItem,
  OverrideItem,
  StagedAction,
  SyncStatus,
  SyncReport,
  SyncHistoryItem,
  AppConfig,
  BackupItem,
  UpdateStatus,
  NotificationItem,
  CurrentUser,
  Candidate,
  MappingResolution,
  IgnoredItem,
  LibraryItem,
  LibraryStats,
} from './types';

import { writable } from 'svelte/store';

let cachedCsrfToken = '';
let cachedApiKey = '';
if (typeof localStorage !== 'undefined') {
  cachedApiKey = localStorage.getItem('anilist_arr_sync_apikey') || '';
}

export const apiKeyStore = writable<string>(cachedApiKey);

export function getCsrfToken(): string {
  if (cachedCsrfToken) return cachedCsrfToken;
  if (typeof document !== 'undefined') {
    const match = document.cookie.match(/(^|;)\s*anilist_sync_csrf=([^;]+)/);
    if (match) {
      cachedCsrfToken = decodeURIComponent(match[2]);
      return cachedCsrfToken;
    }
  }
  return '';
}

export function setCsrfToken(token: string) {
  cachedCsrfToken = token;
}

export function getApiKey(): string {
  if (cachedApiKey) return cachedApiKey;
  if (typeof window !== 'undefined' && window.AniListArrSync?.apiKey) {
    return window.AniListArrSync.apiKey;
  }
  return '';
}

export function setApiKey(key: string) {
  cachedApiKey = key;
  apiKeyStore.set(key);
  if (typeof localStorage !== 'undefined') {
    if (key) {
      localStorage.setItem('anilist_arr_sync_apikey', key);
    } else {
      localStorage.removeItem('anilist_arr_sync_apikey');
    }
  }
}

export function getBaseUrl(): string {
  if (typeof window !== 'undefined' && window.AniListArrSync?.urlBase) {
    const base = window.AniListArrSync.urlBase.trim();
    if (base === '/' || base === '') return '';
    return base.endsWith('/') ? base.slice(0, -1) : base;
  }
  return '';
}

async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const baseUrl = getBaseUrl();
  const url = `${baseUrl}${endpoint}`;

  const headers: Record<string, string> = {
    Accept: 'application/json',
    ...(options.headers as Record<string, string>),
  };

  const apiKey = getApiKey();
  if (apiKey) {
    headers['X-Api-Key'] = apiKey;
  }

  const method = (options.method || 'GET').toUpperCase();
  if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
    headers['X-Requested-With'] = 'XMLHttpRequest';
    const csrf = getCsrfToken();
    if (csrf) {
      headers['X-CSRF-Token'] = csrf;
    }
    if (options.body && typeof options.body === 'string' && !headers['Content-Type']) {
      headers['Content-Type'] = 'application/json';
    }
  }

  const res = await fetch(url, {
    ...options,
    method,
    headers,
  });

  if (!res.ok) {
    let errorMessage = `Request failed with status ${res.status}`;
    let setupRequired: boolean | undefined = undefined;
    try {
      const errData = await res.json();
      if (errData && errData.error) {
        errorMessage = errData.error;
      }
      if (errData && typeof errData.setupRequired === 'boolean') {
        setupRequired = errData.setupRequired;
      }
    } catch {
      // not JSON
    }
    const error = new Error(errorMessage) as Error & { status: number; setupRequired?: boolean };
    error.status = res.status;
    if (setupRequired !== undefined) {
      error.setupRequired = setupRequired;
    }
    throw error;
  }

  const contentType = res.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return (await res.json()) as T;
  }
  return {} as T;
}

export const api = {
  // System Health & Version
  async getHealth(): Promise<{ status: string; version: string }> {
    return request('/api/v1/health');
  },

  // Auth
  async getUser(): Promise<CurrentUser> {
    const user = await request<CurrentUser>('/api/v1/auth/user');
    if (user.csrfToken) {
      setCsrfToken(user.csrfToken);
    }
    return user;
  },

  async login(credentials: { username: string; password: string }): Promise<{ status: string; username: string; csrfToken?: string }> {
    const res = await request<{ status: string; username: string; csrfToken?: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify(credentials),
    });
    if (res.csrfToken) {
      setCsrfToken(res.csrfToken);
    }
    return res;
  },

  async setup(credentials: { username: string; password: string }): Promise<{ status: string; username: string; csrfToken?: string }> {
    const res = await request<{ status: string; username: string; csrfToken?: string }>('/api/v1/auth/setup', {
      method: 'POST',
      body: JSON.stringify(credentials),
    });
    if (res.csrfToken) {
      setCsrfToken(res.csrfToken);
    }
    return res;
  },

  async logout(): Promise<{ status: string }> {
    return request('/api/v1/auth/logout', { method: 'POST' });
  },

  // Sync
  async getSyncStatus(): Promise<SyncStatus> {
    const res = await request<{ running: boolean; lastReport?: SyncReport | null }>('/api/v1/sync/status');
    return {
      isSyncing: !!res.running,
      lastSync: res.lastReport || null,
    };
  },

  async startSync(): Promise<{ status: string }> {
    return request('/api/v1/sync/start', { method: 'POST' });
  },

  async getSyncHistory(): Promise<SyncHistoryItem[]> {
    return request('/api/v1/sync/history');
  },

  // Review Queue
  async getReviewQueue(): Promise<ReviewItem[]> {
    const items = await request<ReviewItem[]>('/api/v1/review');
    return (items || []).map((item) => {
      let candidates: Candidate[] = [];
      if (item.candidatesJson) {
        try {
          candidates = JSON.parse(item.candidatesJson);
        } catch {
          candidates = [];
        }
      }
      return {
        ...item,
        candidates,
      };
    });
  },

  async resolveReview(id: number, resolution: MappingResolution): Promise<{ status: string }> {
    return request(`/api/v1/review/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify(resolution),
    });
  },

  async ignoreReview(id: number): Promise<{ status: string }> {
    return request(`/api/v1/review/${id}/ignore`, {
      method: 'POST',
    });
  },

  // Ignored Titles (Blocklist)
  async getIgnored(): Promise<IgnoredItem[]> {
    return request('/api/v1/ignored');
  },

  async deleteIgnored(anilistId: number): Promise<{ status: string; anilistId: number }> {
    return request(`/api/v1/ignored/${anilistId}`, {
      method: 'DELETE',
    });
  },

  // Library
  async getLibrary(params?: {
    status?: string;
    service?: string;
    monitored?: string;
    search?: string;
    sort?: string;
    order?: string;
  }): Promise<LibraryItem[]> {
    const searchParams = new URLSearchParams();
    if (params) {
      if (params.status && params.status !== 'all') searchParams.set('status', params.status);
      if (params.service && params.service !== 'all') searchParams.set('service', params.service);
      if (params.monitored && params.monitored !== 'all') searchParams.set('monitored', params.monitored);
      if (params.search) searchParams.set('search', params.search);
      if (params.sort) searchParams.set('sort', params.sort);
      if (params.order) searchParams.set('order', params.order);
    }
    const qs = searchParams.toString();
    return request<LibraryItem[]>(`/api/v1/library${qs ? `?${qs}` : ''}`);
  },

  async getLibraryStats(): Promise<LibraryStats> {
    return request<LibraryStats>('/api/v1/library/stats');
  },

  // Overrides
  async getOverrides(): Promise<OverrideItem[]> {
    return request('/api/v1/overrides');
  },

  async createOverride(override: Partial<OverrideItem>): Promise<{ status: string; id?: number }> {
    return request('/api/v1/overrides', {
      method: 'POST',
      body: JSON.stringify(override),
    });
  },

  async deleteOverride(id: number): Promise<{ status: string }> {
    return request(`/api/v1/overrides/${id}`, {
      method: 'DELETE',
    });
  },

  // Staged Actions
  async getStaged(): Promise<StagedAction[]> {
    return request('/api/v1/staged');
  },

  async applyStaged(ids: number[]): Promise<{ status: string; appliedCount: number }> {
    return request('/api/v1/staged/apply', {
      method: 'POST',
      body: JSON.stringify({ ids }),
    });
  },

  async rejectStaged(ids: number[]): Promise<{ status: string; rejectedCount: number }> {
    return request('/api/v1/staged/reject', {
      method: 'POST',
      body: JSON.stringify({ ids }),
    });
  },

  // Config
  async getConfig(): Promise<AppConfig> {
    return request('/api/v1/config');
  },

  async updateConfig(config: Partial<AppConfig>): Promise<{ status: string }> {
    return request('/api/v1/config', {
      method: 'PUT',
      body: JSON.stringify(config),
    });
  },

  // Backups
  async getBackups(): Promise<BackupItem[]> {
    return request('/api/v1/backups');
  },

  async createBackup(): Promise<BackupItem> {
    return request('/api/v1/backups', {
      method: 'POST',
    });
  },

  async restoreBackup(filename: string): Promise<{ status: string }> {
    return request('/api/v1/backups/restore', {
      method: 'POST',
      body: JSON.stringify({ filename }),
    });
  },

  // Updates
  async getUpdateStatus(): Promise<UpdateStatus> {
    return request('/api/v1/update/status');
  },

  async applyUpdate(): Promise<{ status: string }> {
    return request('/api/v1/update/apply', {
      method: 'POST',
    });
  },

  // Notifications
  async getNotifications(): Promise<NotificationItem[]> {
    const list = await request<any[]>('/api/v1/notifications');
    return (list || []).map((item) => {
      let settings: Record<string, unknown> = {};
      if (item.configJson) {
        try {
          settings = JSON.parse(item.configJson);
          if (settings.url && !settings.webhookUrl) {
            settings.webhookUrl = settings.url;
          }
        } catch {}
      } else if (item.settings) {
        settings = item.settings;
      }
      const provider = (item.provider || item.type || 'DISCORD').toUpperCase();
      return {
        ...item,
        type: provider as 'DISCORD' | 'WEBHOOK',
        provider,
        onAdded: item.onAdded ?? true,
        onReview: item.onReview ?? true,
        onError: item.onError ?? true,
        onComplete: item.onComplete ?? false,
        onUpdate: item.onUpdate ?? true,
        onSyncStart: item.onAdded ?? item.onSyncStart ?? true,
        onSyncSuccess: item.onComplete ?? item.onSyncSuccess ?? false,
        onSyncFailure: item.onError ?? item.onSyncFailure ?? true,
        onReviewQueueNeeded: item.onReview ?? item.onReviewQueueNeeded ?? true,
        settings,
      };
    });
  },

  async createNotification(notif: Partial<NotificationItem>): Promise<{ status: string; id?: number }> {
    const provider = (notif.provider || notif.type || 'DISCORD').toUpperCase();
    const webhookUrl = (notif.settings?.webhookUrl || notif.settings?.url || '') as string;
    const payload = {
      name: notif.name,
      provider,
      type: provider,
      configJson: notif.configJson || JSON.stringify({ url: webhookUrl }),
      settings: notif.settings || { webhookUrl },
      url: webhookUrl,
      onAdded: notif.onAdded ?? notif.onSyncStart ?? true,
      onReview: notif.onReview ?? notif.onReviewQueueNeeded ?? true,
      onError: notif.onError ?? notif.onSyncFailure ?? true,
      onComplete: notif.onComplete ?? notif.onSyncSuccess ?? false,
      onUpdate: notif.onUpdate ?? true,
    };
    return request('/api/v1/notifications', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },

  async updateNotification(id: number, notif: Partial<NotificationItem>): Promise<{ status: string }> {
    const provider = (notif.provider || notif.type || 'DISCORD').toUpperCase();
    const webhookUrl = (notif.settings?.webhookUrl || notif.settings?.url || '') as string;
    const payload = {
      name: notif.name,
      provider,
      type: provider,
      configJson: notif.configJson || JSON.stringify({ url: webhookUrl }),
      settings: notif.settings || { webhookUrl },
      url: webhookUrl,
      onAdded: notif.onAdded ?? notif.onSyncStart,
      onReview: notif.onReview ?? notif.onReviewQueueNeeded,
      onError: notif.onError ?? notif.onSyncFailure,
      onComplete: notif.onComplete ?? notif.onSyncSuccess,
      onUpdate: notif.onUpdate,
    };
    return request(`/api/v1/notifications/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  },

  async deleteNotification(id: number): Promise<{ status: string }> {
    return request(`/api/v1/notifications/${id}`, {
      method: 'DELETE',
    });
  },

  async testNotificationPayload(payload: { type: string; settings: Record<string, unknown> }): Promise<{ status: string; message?: string }> {
    const provider = (payload.type || 'DISCORD').toUpperCase();
    const webhookUrl = (payload.settings?.webhookUrl || payload.settings?.url || '') as string;
    return request('/api/v1/notifications/test', {
      method: 'POST',
      body: JSON.stringify({
        provider,
        type: provider,
        configJson: JSON.stringify({ url: webhookUrl }),
        settings: payload.settings,
        url: webhookUrl,
      }),
    });
  },

  async testNotificationConnection(id: number): Promise<{ status: string; message?: string }> {
    return request(`/api/v1/notifications/${id}/test`, {
      method: 'POST',
    });
  },
};
