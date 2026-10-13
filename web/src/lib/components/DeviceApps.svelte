<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { getDeviceApps, type DeviceApp } from '#lib/api/devices';
	import Note from './Note.svelte';

	// Stays mounted across tabs so the apps are fetched once per page view; `tab` picks what to render.
	let { id, tab }: { id: string; tab: string } = $props();

	let apps = $state<DeviceApp[]>([]);
	let cachedAt = $state('');
	let loading = $state(true);
	let error = $state('');
	let q = $state('');

	$effect(() => {
		const deviceId = id;
		loading = true;
		error = '';
		getDeviceApps(deviceId)
			.then((r) => {
				apps = r.apps;
				cachedAt = r.cached ? new Date(r.fetchedAt).toLocaleString() : '';
			})
			.catch((e) => {
				error = e instanceof ApiError ? e.message : 'Could not reach the server.';
			})
			.finally(() => {
				loading = false;
			});
	});

	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		return apps.filter((a) => !needle || `${a.name} ${a.packageName}`.toLowerCase().includes(needle));
	});
</script>

{#if tab === 'summary'}
	<div class="rounded-lg border border-base-300 p-3">
		<div class="flex items-baseline justify-between gap-2">
			<h2 class="text-[18px] font-semibold">Applications{loading || error ? '' : ` (${apps.length})`}</h2>
			<a href="?tab=apps" class="link text-[15px]" data-sveltekit-noscroll>View all</a>
		</div>
		{#if loading}
			<span class="loading loading-spinner"></span>
		{:else if error}
			<p class="muted">{error}</p>
		{:else if apps.length}
			<p class="mt-1 text-[15px] break-words">{apps.slice(0, 5).map((a) => a.name).join(', ')}{apps.length > 5 ? ', …' : ''}</p>
		{:else}
			<p class="muted">No applications</p>
		{/if}
	</div>
{:else if tab === 'apps'}
<section>
	<h2 class="mb-2 text-[20px] font-semibold">Applications</h2>
	{#if loading}
		<span class="loading loading-spinner"></span>
	{:else if error}
		<Note tone="warn">{error}</Note>
	{:else}
		{#if cachedAt}<div class="mb-2"><Note>Knox unreachable — showing data as of {cachedAt}</Note></div>{/if}
		<label class="sr-only" for="app-q">Search applications</label>
		<input id="app-q" type="search" class="field mb-2" placeholder="Search name or package" autocomplete="off" bind:value={q} />
		<p class="muted py-1 text-[15px]" aria-live="polite">{visible.length} {visible.length === 1 ? 'application' : 'applications'}</p>
		<div class="overflow-x-auto">
			<table class="table">
				<thead><tr><th>Name</th><th>Package</th><th>Version</th></tr></thead>
				<tbody>
					{#each visible as a}
						<tr><td>{a.name}</td><td class="break-all">{a.packageName}</td><td>{a.version}</td></tr>
					{:else}
						<tr><td colspan="3" class="muted">No applications</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>
{/if}
