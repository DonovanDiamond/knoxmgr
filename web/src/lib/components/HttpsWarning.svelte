<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api/client';
	import type { Health } from '#lib/api/types';
	import Note from '#components/Note.svelte';

	let needsHttps = $state(false);

	// Secure cookies are dropped by the browser over plain HTTP, so sign-in would silently not stick.
	onMount(async () => {
		const h = await api<Health>('/api/health').catch(() => null);
		const local = ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname);
		needsHttps = !!h?.secureCookies && location.protocol === 'http:' && !local;
	});
</script>

{#if needsHttps}
	<div class="mb-6"><Note tone="warn">Sign-in needs HTTPS here. Put knoxmgr behind HTTPS, or for testing set secureCookies: false in config.yaml.</Note></div>
{/if}
