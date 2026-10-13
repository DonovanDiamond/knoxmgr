import { api, json } from './client';
import { session } from './session.svelte';
import type { AuthResponse } from './types';

export const verifySetupCode = (code: string) => api<void>('/api/setup/verify', json('POST', { code }));

/** Creates the first admin and signs in as them. */
export async function createFirstAdmin(code: string, username: string, password: string): Promise<void> {
	session.set(await api<AuthResponse>('/api/setup/admin', json('POST', { code, username, password })));
}
