import { goto } from '$app/navigation';
import { knoxStatus } from '#lib/health.svelte';
import { session } from './session.svelte';
import type { ApiErrorBody, AuthResponse } from './types';

export class ApiError extends Error {
	constructor(
		message: string,
		readonly status: number,
		readonly code?: string
	) {
		super(message);
	}
}

async function send(path: string, init: RequestInit): Promise<Response> {
	const headers = new Headers(init.headers);
	if (init.body) headers.set('Content-Type', 'application/json');
	if (session.token) headers.set('Authorization', `Bearer ${session.token}`);
	return fetch(path, { ...init, headers, credentials: 'same-origin' });
}

async function fail(res: Response): Promise<never> {
	const body: Partial<ApiErrorBody> = await res.json().catch(() => ({}));
	throw new ApiError(body.error ?? `Request failed (${res.status})`, res.status, body.code);
}

// Concurrent 401s must share one refresh: the refresh cookie rotates on use.
let refreshing: Promise<boolean> | null = null;

export function refresh(): Promise<boolean> {
	refreshing ??= (async () => {
		try {
			const res = await fetch('/api/auth/refresh', { method: 'POST', credentials: 'same-origin' });
			if (!res.ok) return false;
			session.set((await res.json()) as AuthResponse);
			return true;
		} catch {
			return false;
		} finally {
			refreshing = null;
		}
	})();
	return refreshing;
}

/** JSON request. On 401 refreshes once, then clears the session and redirects to /login. */
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
	let res = await send(path, init);
	if (res.status === 401 && session.token) {
		if (await refresh()) {
			res = await send(path, init);
		}
		if (res.status === 401) {
			session.clear();
			await goto('/login');
			return fail(res);
		}
	}
	if (!res.ok) {
		const code = res.status === 409 || res.status === 503 ? await res.clone().json().then((b) => b?.code, () => undefined) : undefined;
		if (code === 'setup_required') await goto('/setup');
		else if (code === 'knox_not_configured') knoxStatus.configured = false;
		return fail(res);
	}
	return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

/** Request init for a JSON body, e.g. api<T>(path, json('POST', data)). */
export function json(method: string, data: unknown): RequestInit {
	return { method, body: JSON.stringify(data) };
}
