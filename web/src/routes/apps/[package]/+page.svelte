<script lang="ts">
	import { page } from '$app/state';
	import { ApiError } from '#lib/api/client';
	import {
		getApp,
		listAppCategories,
		setAppCategory,
		uninstallApp,
		type AppCategory,
		type AppDevice,
		type AppSummary,
		type UninstallResult
	} from '#lib/api/apps';
	import { session } from '#lib/api/session.svelte';
	import AppIcon from '#components/AppIcon.svelte';
	import Avatar from '#components/Avatar.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Note from '#components/Note.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { accessOf, ownerLinkable } from '#lib/perms';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';
	import { SvelteSet } from 'svelte/reactivity';

	const pkg = $derived(page.params.package ?? '');

	let app = $state<AppSummary | null>(null);
	let devices = $state<AppDevice[]>([]);
	let categories = $state<AppCategory[]>([]);
	let loading = $state(true);
	let loadError = $state('');
	let actionError = $state('');

	const selected = new SvelteSet<string>();
	let confirmOpen = $state(false);
	let uninstalling = $state(false);
	let results = $state<UninstallResult[]>([]);

	const isAdmin = $derived(session.user?.role === 'admin');
	const access = $derived(accessOf(session.user, orgs.name(session.user?.orgCode)));
	const title = $derived(app?.name ?? '');
	const selectedDevices = $derived(devices.filter((d) => selected.has(d.deviceId)));

	// Why a device cannot be selected; empty when it can.
	function blocked(d: AppDevice): string {
		if (app?.protected) return app.protectedReason;
		if (!d.android) return 'Only Android devices are supported';
		if (d.uninstallRequested) return 'Uninstall requested';
		return '';
	}
	const label = (d: AppDevice) => d.userName || d.deviceModel || d.deviceId;

	async function load() {
		loading = true;
		loadError = '';
		try {
			const [a, c] = await Promise.all([getApp(pkg), isAdmin ? listAppCategories() : Promise.resolve(null), orgs.load()]);
			app = a.app;
			devices = a.devices;
			if (c) categories = c.categories;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	function toggle(id: string) {
		if (selected.has(id)) selected.delete(id);
		else selected.add(id);
	}

	async function changeCategory(value: string) {
		if (!app) return;
		actionError = '';
		const id = value ? Number(value) : null;
		try {
			await setAppCategory(pkg, id);
			app = { ...app, categoryId: id, category: categories.find((c) => c.id === id)?.name ?? '' };
			toast.show('Category updated');
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		}
	}

	async function uninstall() {
		uninstalling = true;
		actionError = '';
		try {
			results = (await uninstallApp(pkg, selectedDevices.map((d) => d.deviceId))).results;
			const failed = results.filter((r) => !r.ok).length;
			toast.show(failed ? `${failed} of ${results.length} failed` : `Uninstall requested on ${results.length}`);
			selected.clear();
			devices = (await getApp(pkg)).devices;
		} catch (e) {
			confirmOpen = false;
			actionError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			uninstalling = false;
		}
	}

	const deviceName = (id: string) => {
		const d = devices.find((x) => x.deviceId === id);
		return d ? `${d.deviceModel}${d.userName ? ` (${d.userName})` : ''}` : id;
	};

	const when = (d: AppDevice) => (d.lastConnection ? new Date(d.lastConnection).toLocaleString() : 'Never connected');

	const uninstallReason = $derived(
		!access.canWrite ? access.reason : app?.protected ? app.protectedReason : !selected.size ? 'Select devices first' : ''
	);

	usePanel(() => ({
		head: selected.size ? { value: selected.size, label: 'selected' } : { value: devices.length, label: devices.length === 1 ? 'device' : 'devices' },
		note: access.canWrite ? (selected.size ? '' : 'Tap an avatar to select devices to uninstall from.') : access.reason,
		lock: !access.canWrite,
		actions: selected.size
			? [
					{ label: `Uninstall from ${selected.size} ${selected.size === 1 ? 'device' : 'devices'}`, kind: 'danger', lead: true, onclick: () => { results = []; confirmOpen = true; }, disabled: !!uninstallReason, reason: uninstallReason },
					{ label: 'Clear selection', kind: 'soft', onclick: () => selected.clear() }
				]
			: []
	}));
</script>

<BigTitle {title} back="/apps" />

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load this app</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else if app}
	{#if !access.canWrite}
		<div class="mb-4"><Note tone="warn" icon="lock">{access.reason}.</Note></div>
	{/if}
	{#if actionError}<div class="note warn my-3" role="alert"><span>{actionError}</span></div>{/if}

	<div class="mb-4 flex items-center gap-4">
		<AppIcon name={app.name} pkg={app.package} size="lg" />
		<div class="min-w-0">
			<p class="text-[20px] font-bold break-words">{app.name}{#if app.alternateNames.length}{' '}<span class="muted text-[15px] font-normal">({app.alternateNames.join(', ')})</span>{/if}</p>
			<p class="muted text-[15px] break-all">{app.package}</p>
			<div class="mt-1 flex flex-wrap gap-2">
				{#if app.system}<span class="badge">System</span>{/if}
				{#if app.protected}<span class="badge" title={app.protectedReason}>Protected</span>{/if}
			</div>
		</div>
	</div>
	{#if !app.managed}<p class="muted mb-4 text-[15px]">This app is not managed by Knox</p>{/if}

	<div class="mb-4 flex flex-wrap items-end gap-x-8 gap-y-3">
		<div>
			<p class="text-[28px] leading-none font-extrabold tracking-tight">{app.deviceCount}</p>
			<p class="muted mt-1 text-[15px]">{app.deviceCount === 1 ? 'Device' : 'Devices'}</p>
		</div>
		{#if isAdmin}
			<div>
				<label class="muted block text-[15px]" for="cat">Category</label>
				<select id="cat" class="field !w-auto" value={app.categoryId === null ? '' : String(app.categoryId)} onchange={(e) => changeCategory(e.currentTarget.value)}>
					<option value="">Uncategorised</option>
					{#each categories as c (c.id)}<option value={String(c.id)}>{c.name}</option>{/each}
				</select>
			</div>
		{:else}
			<div>
				<p class="text-[18px] font-semibold">{app.category || 'Uncategorised'}</p>
				<p class="muted text-[15px]">Category</p>
			</div>
		{/if}
	</div>

	{#if access.canWrite && app.protected}
		<div class="mb-3"><Note icon="info">{app.protectedReason || 'This app cannot be uninstalled.'}</Note></div>
	{/if}

	<ul>
		{#each devices as d (d.deviceId)}
			{@const why = blocked(d)}
			<li class="row row-hover">
				{#if access.canWrite}
					<span title={why || undefined}>
						<Avatar name={label(d)} seed={d.deviceId} selected={selected.has(d.deviceId)} disabled={!!why} onclick={() => toggle(d.deviceId)} />
					</span>
				{:else}
					<Avatar name={label(d)} seed={d.deviceId} />
				{/if}
				<span class="rowmain">
					<a href="/devices/{encodeURIComponent(d.deviceId)}" class="nm">{d.deviceModel || d.deviceId}</a>
					<span class="sub">
						{#if d.userId}{#if ownerLinkable(d.userId)}<a href="/users/{encodeURIComponent(d.userId)}" class="link">{d.userName || d.userId}</a>{:else}{d.userName || d.userId}{/if}{' · '}{/if}{d.platform} · v{d.version || '?'}
					</span>
					<span class="sub">Last connection: {when(d)}</span>
					{#if access.canWrite && why && !d.uninstallRequested}<span class="sub">{why}</span>{/if}
				</span>
				{#if d.uninstallRequested}<span class="badge badge-warning">Uninstall requested</span>{/if}
				<DeviceBadge freshness={d.freshness} />
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">Not installed anywhere</p>
				<p class="muted mt-1">No synced device reports this app.</p>
			</li>
		{/each}
	</ul>
{/if}

<Dialog bind:open={confirmOpen} title={results.length ? 'Uninstall results' : `Uninstall ${title}?`}>
	{#if results.length}
		<ul class="mt-3 max-h-[40vh] overflow-y-auto">
			{#each results as r (r.deviceId)}
				<li class="py-1">
					<span class="font-semibold">{deviceName(r.deviceId)}</span>:
					{#if r.ok}Uninstall requested{:else}<span class="err">{r.error || 'Failed'}</span>{/if}
				</li>
			{/each}
		</ul>
		<div class="mt-5 flex justify-end"><button type="button" class="btn btn-primary" onclick={() => (confirmOpen = false)}>Done</button></div>
	{:else}
		<p class="muted mt-1">
			{title} will be uninstalled from {selectedDevices.length} {selectedDevices.length === 1 ? 'device' : 'devices'} in Knox.
		</p>
		<div class="mt-5 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
			<button type="button" class="btn btn-soft2" onclick={() => (confirmOpen = false)}>Cancel</button>
			<button type="button" class="btn btn-danger" disabled={uninstalling} onclick={uninstall}>Uninstall from {selectedDevices.length} {selectedDevices.length === 1 ? 'device' : 'devices'}</button>
		</div>
	{/if}
</Dialog>
