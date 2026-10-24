<script lang="ts">
	import { ownerLinkable } from '#lib/perms';
	import { ApiError } from '#lib/api/client';
	import { listDevices, type Device } from '#lib/api/devices';
	import KnoxOffline from '#components/KnoxOffline.svelte';
	import { knoxStatus } from '#lib/health.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';

	let devices = $state<Device[]>([]);
	let loading = $state(true);
	let loadError = $state('');

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

	const enrolled = $derived(devices.filter((d) => d.enrolled));
	// Freshness is computed by the API; "none"/"never" mean no connection on record. Stalest first.
	const stale = $derived(
		enrolled
			.filter((d) => ['stale', 'old', 'never', 'none'].includes(d.freshness))
			.sort((a, b) => (a.lastConnection ? Date.parse(a.lastConnection) : 0) - (b.lastConnection ? Date.parse(b.lastConnection) : 0))
	);

	const when = (d: Device) => (d.lastConnection ? new Date(d.lastConnection).toLocaleString() : 'Never connected');
</script>

<BigTitle title="Home" />

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
	<a href="/devices" class="panel mt-2 mb-6 block max-w-xs">
		<p class="text-[40px] leading-none font-extrabold tracking-tight">{devices.length}</p>
		<p class="muted mt-1">{devices.length === 1 ? 'device' : 'devices'} · {enrolled.length} enrolled</p>
	</a>

	<h2 class="text-[20px] font-bold">Not connected in over 24 hours</h2>
	<ul class="mt-2">
		{#each stale as d (d.deviceId)}
			<li class="row row-hover relative">
				<a href="/devices/{encodeURIComponent(d.deviceId)}" class="rowmain after:absolute after:inset-0">
					<span class="nm">{d.deviceName || d.modelName}</span>
					<span class="sub">Last connection: {when(d)}</span>
				</a>
				{#if d.userId}
					{#if ownerLinkable(d.userId)}
						<a href="/users/{encodeURIComponent(d.userId)}" class="link relative z-10">{d.userName || d.userId}</a>
					{:else}
						<span class="relative z-10">{d.userName || d.userId}</span>
					{/if}
				{/if}
				<DeviceBadge freshness={d.freshness} />
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">All devices are up to date</p>
				<p class="muted mt-1">Every enrolled device has connected in the last 24 hours.</p>
			</li>
		{/each}
	</ul>
{/if}
