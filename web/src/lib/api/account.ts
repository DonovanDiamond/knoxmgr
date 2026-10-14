import { api } from './client';
import type { User } from './types';

export const updateMe = (displayName: string) =>
	api<User>('/api/auth/me', { method: 'PUT', body: JSON.stringify({ displayName }) });

export const changePassword = (currentPassword: string, newPassword: string) =>
	api<void>('/api/auth/me/password', { method: 'POST', body: JSON.stringify({ currentPassword, newPassword }) });
