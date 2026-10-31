import { api, json } from './client';
import type {
	AdSettings,
	AdSettingsInput,
	AdTestResult,
	GeneralSettings,
	KnoxSettings,
	KnoxSettingsInput,
	KnoxTestResult,
	ProxySettings,
	ProxySettingsInput,
	UpdateStatus
} from './types';

const base = '/api/admin/settings';

export const getKnoxSettings = () => api<KnoxSettings>(`${base}/knox`);
export const saveKnoxSettings = (s: KnoxSettingsInput) => api<KnoxSettings>(`${base}/knox`, json('PUT', s));
export const testKnoxSettings = (s: KnoxSettingsInput) => api<KnoxTestResult>(`${base}/knox/test`, json('POST', s));

export const getAdSettings = () => api<AdSettings>(`${base}/ad`);
export const saveAdSettings = (s: AdSettingsInput) => api<AdSettings>(`${base}/ad`, json('PUT', s));
/** settings is the current unsaved form, so the test runs against what is on screen. */
export const testAdSignIn = (username: string, password: string, settings?: AdSettingsInput) =>
	api<AdTestResult>(`${base}/ad/test`, json('POST', { username, password, settings }));

export const getGeneralSettings = () => api<GeneralSettings>(`${base}/general`);
export const saveGeneralSettings = (s: GeneralSettings) => api<GeneralSettings>(`${base}/general`, json('PUT', s));

export type SyncRun = {
	id: number;
	trigger: string;
	startedAt: string;
	finishedAt: string | null;
	status: 'running' | 'ok' | 'failed';
	error: string;
	counts: { orgs: number; users: number; groups: number; memberships: number; devices: number };
};
export type SyncState = { running: boolean; runId?: number; runs: SyncRun[] };

/** Runs are newest first. */
export const getSyncState = () => api<SyncState>('/api/admin/sync');
export const syncNow = () => api<{ id: number; alreadyRunning: boolean }>('/api/admin/sync', { method: 'POST' });

export const getUpdateStatus = () => api<UpdateStatus>('/api/admin/update');
export const checkForUpdates = () => api<UpdateStatus>('/api/admin/update/check', { method: 'POST' });

export const getProxySettings = () => api<ProxySettings>(`${base}/proxy`);
export const saveProxySettings = (s: ProxySettingsInput) => api<ProxySettings>(`${base}/proxy`, json('PUT', s));
