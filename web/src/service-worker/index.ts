/// <reference types="@sveltejs/kit" />
/// <reference lib="webworker" />
import { version } from '$app/environment';
import { assets, immutable } from '$app/manifest';

const sw = self as unknown as ServiceWorkerGlobalScope;
const CACHE = `shell-${version}`;
// Hashed build output plus static icons only. API responses are never cached (they are per-user and authenticated).
const SHELL = [...immutable, ...assets].map((f) => '/' + f.path);
// The SPA fallback page, so an offline navigation still boots the router.
const INDEX = '/index.html';

sw.addEventListener('install', (e) => {
	e.waitUntil(caches.open(CACHE).then((c) => c.addAll([...SHELL, INDEX])).then(() => sw.skipWaiting()));
});

sw.addEventListener('activate', (e) => {
	e.waitUntil(
		caches
			.keys()
			.then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
			.then(() => sw.clients.claim())
	);
});

sw.addEventListener('fetch', (e) => {
	const req = e.request;
	const url = new URL(req.url);
	if (req.method !== 'GET' || url.origin !== sw.location.origin || url.pathname.startsWith('/api/')) return;

	if (req.mode === 'navigate') {
		// Network-first so a deploy is seen immediately; the cached shell only matters offline.
		e.respondWith(fetch(req).catch(async () => (await caches.match(INDEX)) ?? Response.error()));
		return;
	}
	if (SHELL.includes(url.pathname)) {
		e.respondWith(caches.match(req).then((hit) => hit ?? fetch(req)));
	}
});
