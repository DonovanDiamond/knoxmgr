import { api, json } from './client';
import type { Freshness } from './devices';

const seg = encodeURIComponent;

export interface AppSummary {
	package: string;
	name: string;
	alternateNames: string[];
	system: boolean;
	managed: boolean;
	mandatory: boolean;
	protected: boolean;
	protectedReason: string;
	category: string;
	categoryId: number | null;
	deviceCount: number;
}

export interface AppDevice {
	deviceId: string;
	deviceModel: string;
	userId: string;
	userName: string;
	platform: string;
	android: boolean;
	version: string;
	managed: boolean;
	lastConnection: string | null;
	freshness: Freshness;
	uninstallRequested: boolean;
}

export interface AppCategory {
	id: number;
	name: string;
	builtIn: boolean;
	appCount: number;
}

export interface UninstallResult {
	deviceId: string;
	ok: boolean;
	error?: string;
}

export interface DeviceAppRow {
	package: string;
	name: string;
	version: string;
	system: boolean;
	managed: boolean;
	mandatory: boolean;
	protected: boolean;
	protectedReason: string;
	installedAt: string | null;
	category: string;
	categoryId: number | null;
	uninstallRequested: boolean;
}

export interface DeviceAppsResponse {
	apps: DeviceAppRow[];
	syncedAt: string | null;
	stale: boolean;
	android: boolean;
}

export function listApps(opts: { search?: string; category?: string; system?: boolean } = {}) {
	const p = new URLSearchParams();
	if (opts.search) p.set('search', opts.search);
	if (opts.category) p.set('category', opts.category);
	if (opts.system) p.set('system', 'true');
	const qs = p.toString();
	return api<{ apps: AppSummary[] }>(`/api/knox/apps${qs ? `?${qs}` : ''}`);
}

export const getApp = (pkg: string) =>
	api<{ app: AppSummary; devices: AppDevice[] }>(`/api/knox/apps/${seg(pkg)}`);

export const uninstallApp = (pkg: string, deviceIds: string[]) =>
	api<{ results: UninstallResult[] }>(`/api/knox/apps/${seg(pkg)}/uninstall`, json('POST', { deviceIds }));

export const listAppCategories = () => api<{ categories: AppCategory[] }>('/api/knox/app-categories');

export const createAppCategory = (name: string) => api<AppCategory>('/api/admin/app-categories', json('POST', { name }));

export const renameAppCategory = (id: number, name: string) =>
	api<AppCategory>(`/api/admin/app-categories/${id}`, json('PUT', { name }));

export const deleteAppCategory = (id: number) => api<void>(`/api/admin/app-categories/${id}`, { method: 'DELETE' });

export const setAppCategory = (pkg: string, categoryId: number | null) =>
	api<void>(`/api/admin/apps/${seg(pkg)}/category`, json('PUT', { categoryId }));

export const getDeviceApps = (deviceId: string) => api<DeviceAppsResponse>(`/api/knox/devices/${seg(deviceId)}/apps`);

export const refreshDeviceApps = (deviceId: string) =>
	api<{ requested: boolean }>(`/api/knox/devices/${seg(deviceId)}/apps/refresh`, { method: 'POST' });

// Category group order used on every screen: alphabetical, then "Other", then "System" last.
const groupRank = (name: string) => (name === 'System' ? 2 : name === 'Other' ? 1 : 0);
export const compareCategories = (a: string, b: string) => groupRank(a) - groupRank(b) || a.localeCompare(b);
