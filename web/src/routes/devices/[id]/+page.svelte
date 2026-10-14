<script lang="ts">
	import { ownerLinkable } from '#lib/perms';
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { ApiError } from '#lib/api/client';
	import { getDevice, getDeviceDetail, type Device, type DeviceDetail } from '#lib/api/devices';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';
	import Note from '#components/Note.svelte';
	import DeviceApps from '#components/DeviceApps.svelte';
	import DeviceLocation from '#components/DeviceLocation.svelte';

	// A derived string only changes with the device, so switching ?tab= does not reload anything.
	const deviceKey = $derived(page.params.id ?? '');

	let device = $state<Device | null>(null);
	let loading = $state(true);
	let loadError = $state('');

	async function load() {
		loading = true;
		loadError = '';
		try {
			device = await getDevice(deviceKey);
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		deviceKey;
		untrack(load);
	});

	let detail = $state<DeviceDetail | null>(null);
	let detailError = $state('');
	$effect(() => {
		const id = deviceKey;
		detail = null;
		detailError = '';
		getDeviceDetail(id)
			.then((r) => (detail = r))
			.catch((e) => {
				detailError = e instanceof ApiError ? e.message : 'Could not reach the server.';
			});
	});

	const tabs = $derived<[string, string][]>([
		['summary', 'Summary'],
		['policies', 'Policies'],
		['apps', 'Applications'],
		...(detail?.canViewLocation ? [['location', 'Location'] as [string, string]] : [])
	]);
	// Unknown or unavailable tabs fall back to the summary.
	const tab = $derived.by(() => {
		const t = page.url.searchParams.get('tab') ?? '';
		return tabs.some(([k]) => k === t) ? t : 'summary';
	});

	const fmtDate = (iso?: string) => (iso ? new Date(iso).toLocaleString() : '');
	const bool = (v?: boolean) => (v === undefined ? '' : v ? 'Yes' : 'No');

	// Label/value pairs after the enrolled user and status cells; empty values are dropped.
	const fields = $derived.by(() => {
		if (!device) return [];
		const i = detail?.info ?? {};
		const os = i.os || device.os;
		const ver = i.osVersion || device.osVersion;
		const model = i.modelName || device.modelName;
		const all: [string, string][] = [
			['Model', i.model && i.model !== model ? `${model} (${i.model})` : model],
			['OS', [os, ver].filter(Boolean).join(' ')],
			['Serial number', i.serialNumber ?? ''],
			['IMEI', i.imei ?? ''],
			['Phone number', i.phoneNumber ?? ''],
			['Carrier', i.carrier ?? ''],
			['Battery', i.batteryLevel === undefined ? '' : `${i.batteryLevel}`],
			['Rooted', bool(i.rooted)],
			['Agent / client profile', [i.agentVersion, i.clientVersion].filter(Boolean).join(' / ')],
			['Created', fmtDate(i.createdAt)],
			['Device ID', device.deviceId]
		];
		return all.filter(([, v]) => v !== '');
	});
</script>

<BigTitle title={device ? device.deviceName || device.modelName : 'Device'} back="/devices" />

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load this device</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else if device}
	{#if detailError}
		<div class="mb-3"><Note tone="warn">{detailError}</Note></div>
	{:else if detail?.cached}
		<div class="mb-3"><Note>Knox unreachable — showing data as of {new Date(detail.fetchedAt).toLocaleString()}</Note></div>
	{/if}
	<dl class="grid grid-cols-2 gap-x-4 gap-y-3 lg:grid-cols-4">
		{#if device.userId}
			<div class="min-w-0">
				<dt class="muted text-[15px]">Enrolled user</dt>
				<dd class="text-[18px] break-words">{#if ownerLinkable(device.userId)}<a href="/users/{encodeURIComponent(device.userId)}" class="link">{device.userName || device.userId}</a>{:else}{device.userName || device.userId}{/if}</dd>
			</div>
		{/if}
		<div class="min-w-0">
			<dt class="muted text-[15px]">Status</dt>
			<dd class="text-[18px]">
				{device.status}
				<DeviceBadge freshness={device.freshness} title={device.lastConnection ? `Last connection ${fmtDate(device.lastConnection)}` : undefined} />
			</dd>
		</div>
		{#each fields as [label, value] (label)}
			<div class="min-w-0">
				<dt class="muted text-[15px]">{label}</dt>
				<dd class="text-[18px] break-words">{value}</dd>
			</div>
		{/each}
	</dl>
	{#if device.lastConnection}<p class="muted mt-2 text-[15px]">Last connection {fmtDate(device.lastConnection)}</p>{/if}

	<div role="tablist" class="tabs tabs-border mt-6 overflow-x-auto flex-nowrap">
		{#each tabs as [key, label] (key)}
			<a role="tab" href="?tab={key}" class="tab whitespace-nowrap {tab === key ? 'tab-active' : ''}" aria-selected={tab === key} data-sveltekit-noscroll>{label}</a>
		{/each}
	</div>

	<div class="mt-4 {tab === 'summary' ? 'grid gap-3 md:grid-cols-2 lg:grid-cols-3' : ''}">
		{#if tab === 'summary' || tab === 'policies'}
			{#if !detail}
				<div><span class="loading loading-spinner"></span></div>
			{:else if tab === 'summary'}
				<div class="rounded-lg border border-base-300 p-3">
					<div class="flex items-baseline justify-between gap-2">
						<h2 class="text-[18px] font-semibold">Policies ({detail.profiles.length})</h2>
						<a href="?tab=policies" class="link text-[15px]" data-sveltekit-noscroll>View all</a>
					</div>
					{#if detail.profiles.length}
						<ul class="mt-1 flex flex-wrap gap-2">
							{#each detail.profiles.slice(0, 4) as p (p)}<li class="badge badge-outline h-auto py-1 text-[15px]">{p}</li>{/each}
							{#if detail.profiles.length > 4}<li class="muted text-[15px]">+{detail.profiles.length - 4} more</li>{/if}
						</ul>
					{:else}
						<p class="muted">No policies</p>
					{/if}
				</div>
			{:else}
				<section>
					<h2 class="mb-2 text-[20px] font-semibold">Policies</h2>
					{#if detail.profiles.length}
						<ul class="flex flex-wrap gap-2">
							{#each detail.profiles as p (p)}<li class="badge badge-outline h-auto py-1 text-[15px]">{p}</li>{/each}
						</ul>
					{:else}
						<p class="muted">No policies</p>
					{/if}
				</section>
			{/if}
		{/if}
		<DeviceApps id={device.deviceId} {tab} />
		{#if detail?.canViewLocation}<DeviceLocation id={device.deviceId} {tab} />{/if}
	</div>
{/if}
