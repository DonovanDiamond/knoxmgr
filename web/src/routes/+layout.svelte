<script lang="ts">
	import '@fontsource/figtree/latin-400.css';
	import '@fontsource/figtree/latin-500.css';
	import '@fontsource/figtree/latin-600.css';
	import '@fontsource/figtree/latin-800.css';
	import '../app.css';
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { restore } from '#lib/api/auth';
	import { api } from '#lib/api/client';
	import type { Health } from '#lib/api/types';
	import { knoxStatus } from '#lib/health.svelte';
	import { shell } from '#lib/shell.svelte';
	import { session } from '#lib/api/session.svelte';
	import Shell from '#components/Shell.svelte';
	import { onMount } from 'svelte';

	let { children } = $props();
	let ready = $state(false);

	// SvelteKit does not say whether a navigation replaced the entry, so replaceState gotos over-count by one.
	afterNavigate((nav) => {
		if (nav.type === 'enter') shell.depth = 0;
		else if (nav.type === 'popstate') shell.depth = Math.max(0, shell.depth + nav.delta);
		else shell.depth++;
	});

	onMount(async () => {
		const health = await api<Health>('/api/health').catch(() => null);
		if (health) knoxStatus.configured = health.knoxConfigured;
		// A fresh install has no accounts, so setup comes before any session restore or login.
		if (health?.setupRequired) {
			if (page.url.pathname !== '/setup') await goto('/setup');
			ready = true;
			return;
		}
		const signedIn = !!session.user || (await restore());
		const onLogin = page.url.pathname === '/login';
		if (page.url.pathname === '/setup') await goto(signedIn ? '/users' : '/login');
		else if (!signedIn && !onLogin) await goto('/login');
		else if (signedIn && onLogin) await goto('/users');
		ready = true;
	});
</script>

{#if !ready}
	<div class="flex min-h-screen items-center justify-center">
		<span class="loading loading-spinner loading-lg"></span>
	</div>
{:else if ['/login', '/setup'].includes(page.url.pathname)}
	{@render children()}
{:else}
	<Shell>{@render children()}</Shell>
{/if}
