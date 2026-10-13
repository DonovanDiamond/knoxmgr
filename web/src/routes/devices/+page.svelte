<script lang="ts">
	import { page } from '$app/state';
	import { ApiError } from '#lib/api/client';
	import { listDevices, type Device } from '#lib/api/devices';
	import KnoxOffline from '#components/KnoxOffline.svelte';
	import { knoxStatus } from '#lib/health.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Icon from '#components/Icon.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';

	let devices = $state<Device[]>([]);
	let loading = $state(true);
	let loadError = $state('');
	let q = $state('');

	async function load() {
		loading = true;
		loadError = '';
		try {
			devices = (await listDevices()).devices;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	const filters: [string, string][] = [
		['enrolled', 'Enrolled'],
		['unenrolled', 'Unenrolled'],
		['all', 'All']
	];
	// Unknown values fall back to enrolled.
	const filter = $derived.by(() => {
		const f = page.url.searchParams.get('filter') ?? '';
		return filters.some(([k]) => k === f) ? f : 'enrolled';
	});

	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		return devices.filter(
			(d) =>
				(filter === 'all' || d.enrolled === (filter === 'enrolled')) &&
				(!needle ||
				`${d.modelName} ${d.deviceName ?? ''} ${d.os} ${d.userName ?? ''} ${d.userId ?? ''}`.toLowerCase().includes(needle))
		);
	});

	const when = (d: Device) =>
		d.lastConnection ? new Date(d.lastConnection).toLocaleString() : d.freshness === 'not_enrolled' ? 'Not enrolled' : 'Never connected';
</script>

<BigTitle title="Devices" />

<div class="relative mb-2">
	<span class="muted absolute top-1/2 left-3 -translate-y-1/2"><Icon name="search" size={20} /></span>
	<label class="sr-only" for="q">Search devices</label>
	<input id="q" type="search" class="field !pl-10" placeholder="Search model, name, OS or user" autocomplete="off" bind:value={q} />
</div>

<div class="mb-3 inline-flex max-w-full gap-1 rounded-full bg-base-200 p-1" role="group" aria-label="Filter devices">
	{#each filters as [key, label] (key)}
		<a
			href="?filter={key}"
			class="flex min-h-10 items-center rounded-full px-4 font-semibold outline-offset-2 focus-visible:outline-2 focus-visible:outline-primary {filter === key ? 'bg-base-100 text-primary shadow-sm' : 'text-[var(--muted)] hover:text-base-content'}"
			aria-current={filter === key ? 'true' : undefined}
			data-sveltekit-noscroll>{label}</a
		>
	{/each}
</div>

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if knoxStatus.configured === false}
	<KnoxOffline />
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load devices</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<p class="muted py-2 text-[15px]" aria-live="polite">
		{visible.length} {visible.length === 1 ? 'device' : 'devices'}{q ? ' match' : ''}
	</p>
	<ul>
		{#each visible as d (d.deviceId)}
			<li class="row row-hover relative">
				<a href="/devices/{encodeURIComponent(d.deviceId)}" class="rowmain after:absolute after:inset-0">
					<span class="nm">{d.deviceName || d.modelName}</span>
					<span class="sub">{d.deviceName ? `${d.modelName} · ` : ''}{d.os}{d.osVersion ? ` ${d.osVersion}` : ''} · {d.status}</span>
					<span class="sub">Last connection: {when(d)}</span>
				</a>
				{#if d.userId}
					<a href="/users/{encodeURIComponent(d.userId)}" class="link relative z-10">{d.userName || d.userId}</a>
				{/if}
				<DeviceBadge freshness={d.freshness} />
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">{devices.length ? 'No device matches' : 'No devices yet'}</p>
				<p class="muted mt-1">{devices.length ? 'Check the spelling, or change the filter.' : 'Enrolled devices will appear here.'}</p>
			</li>
		{/each}
	</ul>
{/if}
