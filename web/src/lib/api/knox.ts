import { api, json } from './client';
import type {
	CreateKnoxUser,
	EnrollmentResult,
	Group,
	KnoxUser,
	KnoxUserFields,
	Organization
} from './types';

const seg = encodeURIComponent;

export const listOrgs = () => api<{ orgs: Organization[] }>('/api/knox/orgs').then((r) => r.orgs);

export const listUsers = (q = '') =>
	api<{ users: KnoxUser[]; total: number }>(`/api/knox/users${q ? `?q=${seg(q)}` : ''}`).then((r) => r.users);
export const getUser = (id: string) => api<KnoxUser>(`/api/knox/users/${seg(id)}`);
export const createUser = (u: CreateKnoxUser) =>
	api<{ userId: string }>('/api/knox/users', json('POST', u));
export const updateUser = (id: string, fields: KnoxUserFields) =>
	api<{ userId: string }>(`/api/knox/users/${seg(id)}`, json('PUT', fields));
/** Irreversible. The server rejects the call unless confirmUserId equals id. */
export const purgeUser = (id: string, confirmUserId: string) =>
	api<void>(`/api/knox/users/${seg(id)}`, json('DELETE', { confirmUserId }));
export const inviteUsers = (userIds: string[]) =>
	api<EnrollmentResult>('/api/knox/users/invite', json('POST', { userIds }));

export const listGroups = () =>
	api<{ groups: Group[]; total: number }>('/api/knox/groups').then((r) => r.groups);
export const getGroup = (id: string) => api<Group>(`/api/knox/groups/${seg(id)}`);
export const createGroup = (name: string) => api<Group>('/api/knox/groups', json('POST', { name }));
export const deleteGroup = (id: string) => api<void>(`/api/knox/groups/${seg(id)}`, { method: 'DELETE' });
/** Members are filtered to the caller's scope by the server. */
export const listMembers = (id: string) =>
	api<{ users: KnoxUser[]; total: number }>(`/api/knox/groups/${seg(id)}/members`).then((r) => r.users);
export const addMembers = (id: string, userIds: string[]) =>
	api<void>(`/api/knox/groups/${seg(id)}/members`, json('POST', { userIds }));
export const removeMembers = (id: string, userIds: string[]) =>
	api<void>(`/api/knox/groups/${seg(id)}/members`, json('DELETE', { userIds }));
/** Served from the local mirror in one request. */
export const getUserGroups = (userId: string) => api<{ groups: Group[]; total: number }>(`/api/knox/users/${seg(userId)}/groups`).then((r) => r.groups);
