import { goto } from '$app/navigation';
import { api, refresh } from './client';
import { session } from './session.svelte';
import type { AuthResponse } from './types';

export async function login(username: string, password: string): Promise<void> {
	session.set(
		await api<AuthResponse>('/api/auth/login', {
			method: 'POST',
			body: JSON.stringify({ username, password })
		})
	);
}

export async function logout(): Promise<void> {
	try {
		await api<void>('/api/auth/logout', { method: 'POST' });
	} finally {
		session.clear();
		await goto('/login');
	}
}

/** Restores the session from the refresh cookie after a page load. */
export const restore = refresh;
