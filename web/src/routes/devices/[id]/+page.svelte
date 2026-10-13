<script lang="ts">
	import { page } from '$app/state';
	import { ApiError } from '#lib/api/client';
	import { getDevice, getDeviceDetail, type Device, type DeviceDetail } from '#lib/api/devices';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';
	import Note from '#components/Note.svelte';
	import DeviceApps from '#components/DeviceApps.svelte';
	import DeviceLocation from '#components/DeviceLocation.svelte';

	let device = $state<Device | null>(null);
	let loading = $state(true);
	let loadError = $state('');

	async function load() {
		loading = true;
		loadError = '';
		try {
			device = await getDevice(page.params.id ?? '');
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	let detail = $state<DeviceDetail | null>(null);
	let detailError = $state('');
	$effect(() => {
		const id = page.params.id ?? '';
		detail = null;
		detailError = '';
		getDeviceDetail(id)
			.then((r) => (detail = r))
			.catch((e) => {
				detailError = e instanceof ApiError ? e.message : 'Could not reach the server.';
			});
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
				<dd class="text-[18px] break-words"><a href="/users/{encodeURIComponent(device.userId)}" class="link">{device.userName || device.userId}</a></dd>
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

	{#if detail}
		<section class="mt-8">
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

	<DeviceApps id={device.deviceId} />
	{#if detail?.canViewLocation}<DeviceLocation id={device.deviceId} />{/if}
{/if}
