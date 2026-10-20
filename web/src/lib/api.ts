import type {
  ReviewItem,
  OverrideItem,
  StagedAction,
  SyncStatus,
  SyncHistoryItem,
  AppConfig,
  BackupItem,
  UpdateStatus,
  NotificationItem,
  CurrentUser,
  Candidate,
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

  const method = (options.method || 'GET').toUpperCase();
  if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
    const csrf = getCsrfToken();
    if (csrf) {
      headers['X-CSRF-Token'] = csrf;
    }
    const apiKey = getApiKey();
    if (apiKey) {
      headers['X-Api-Key'] = apiKey;
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
    try {
      const errData = await res.json();
      if (errData && errData.error) {
        errorMessage = errData.error;
      }
    } catch {
      // not JSON
    }
    const error = new Error(errorMessage) as Error & { status: number };
    error.status = res.status;
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
    return request('/api/v1/sync/status');
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
    return items.map((item) => {
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

  async resolveReview(id: number, resolution: { mediaType: 'SERIES' | 'MOVIE'; tvdbId?: number | null; tmdbId?: number | null; seasons?: string; titleOverride?: string }): Promise<{ status: string }> {
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
    return request('/api/v1/notifications');
  },

  async createNotification(notif: Partial<NotificationItem>): Promise<{ status: string; id?: number }> {
    return request('/api/v1/notifications', {
      method: 'POST',
      body: JSON.stringify(notif),
    });
  },

  async updateNotification(id: number, notif: Partial<NotificationItem>): Promise<{ status: string }> {
    return request(`/api/v1/notifications/${id}`, {
      method: 'PUT',
      body: JSON.stringify(notif),
    });
  },

  async deleteNotification(id: number): Promise<{ status: string }> {
    return request(`/api/v1/notifications/${id}`, {
      method: 'DELETE',
    });
  },

  async testNotificationPayload(payload: { type: string; settings: Record<string, any> }): Promise<{ status: string; message?: string }> {
    return request('/api/v1/notifications/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },

  async testNotificationConnection(id: number): Promise<{ status: string; message?: string }> {
    return request(`/api/v1/notifications/${id}/test`, {
      method: 'POST',
    });
  },
};
