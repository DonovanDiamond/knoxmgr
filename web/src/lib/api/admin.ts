import { api, json } from './client';
import type { AppUser, AuditEntry, CreateAppUser, UpdateAppUser } from './types';

const seg = encodeURIComponent;

export const listAppUsers = () =>
	api<{ users: AppUser[]; total: number }>('/api/admin/users').then((r) => r.users);
export const getAppUser = (id: number) => api<AppUser>(`/api/admin/users/${seg(String(id))}`);
export const createAppUser = (u: CreateAppUser) => api<AppUser>('/api/admin/users', json('POST', u));
export const updateAppUser = (id: number, fields: UpdateAppUser) =>
	api<AppUser>(`/api/admin/users/${seg(String(id))}`, json('PUT', fields));
export const deleteAppUser = (id: number) =>
	api<void>(`/api/admin/users/${seg(String(id))}`, { method: 'DELETE' });

/** Newest first; the server allows a limit of 1 to 1000 (default 100). */
export const listAudit = (limit?: number) =>
	api<{ entries: AuditEntry[] }>(`/api/admin/audit${limit ? `?limit=${limit}` : ''}`).then((r) => r.entries);
