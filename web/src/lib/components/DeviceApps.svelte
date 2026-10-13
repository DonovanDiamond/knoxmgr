<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { compareCategories, getDeviceApps, refreshDeviceApps, uninstallApp, type DeviceAppRow } from '#lib/api/apps';
	import { session } from '#lib/api/session.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { accessOf } from '#lib/perms';
	import { toast } from '#lib/toast.svelte';
	import AppIcon from './AppIcon.svelte';
	import Dialog from './Dialog.svelte';
	import Note from './Note.svelte';

	// Stays mounted across tabs so the apps are fetched once per page view; `tab` picks what to render.
	let { id, tab }: { id: string; tab: string } = $props();

	let apps = $state<DeviceAppRow[]>([]);
	let syncedAt = $state<string | null>(null);
	let stale = $state(false);
	let android = $state(true);
	let loading = $state(true);
	let error = $state('');
	let q = $state('');
	let showSystem = $state(false);

	const access = $derived(accessOf(session.user, orgs.name(session.user?.orgCode)));

	async function load(deviceId: string) {
		loading = true;
		error = '';
		try {
			const r = await getDeviceApps(deviceId);
			apps = r.apps;
			syncedAt = r.syncedAt;
			stale = r.stale;
			android = r.android;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load(id);
	});

	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		return apps.filter((a) => (showSystem || !a.system) && (!needle || `${a.name} ${a.package}`.toLowerCase().includes(needle)));
	});

	const groups = $derived.by(() => {
		const m = new Map<string, DeviceAppRow[]>();
		for (const a of visible) {
			const k = a.category || 'Other';
			m.set(k, [...(m.get(k) ?? []), a]);
		}
		return [...m].sort(([a], [b]) => compareCategories(a, b));
	});

	const when = (iso: string) => new Date(iso).toLocaleString();

	let refreshing = $state(false);
	async function refresh() {
		refreshing = true;
		try {
			await refreshDeviceApps(id);
			toast.show('Request sent — list updates after the device reports');
		} catch (e) {
			toast.show(e instanceof ApiError ? e.message : 'Could not reach the server.');
		} finally {
			refreshing = false;
		}
	}

	let target = $state<DeviceAppRow | null>(null);
	let confirmOpen = $state(false);
	let busy = $state(false);
	let uninstallError = $state('');

	function askUninstall(a: DeviceAppRow) {
		target = a;
		uninstallError = '';
		confirmOpen = true;
	}
	async function uninstall() {
		if (!target) return;
		busy = true;
		uninstallError = '';
		try {
			const { results } = await uninstallApp(target.package, [id]);
			const failed = results.find((r) => !r.ok);
			if (failed) {
				uninstallError = failed.error || 'Knox rejected the request.';
				return;
			}
			confirmOpen = false;
			toast.show('Uninstall requested');
			await load(id);
		} catch (e) {
			uninstallError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			busy = false;
		}
	}

	const uninstallReason = (a: DeviceAppRow) =>
		!access.canWrite ? access.reason : !android ? 'Only Android devices support this' : a.protected ? a.protectedReason : '';
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
	<div class="mb-2 flex flex-wrap items-center gap-x-3 gap-y-2">
		<h2 class="text-[20px] font-semibold">Applications</h2>
		{#if access.canWrite && android}
			<button type="button" class="btn btn-soft2 ml-auto" disabled={refreshing} onclick={refresh}>
				{refreshing ? 'Requesting...' : 'Refresh from device'}
			</button>
		{/if}
	</div>
	{#if loading}
		<span class="loading loading-spinner"></span>
	{:else if error}
		<Note tone="warn">{error}</Note>
	{:else}
		{#if syncedAt}<p class="muted mb-2 text-[15px]">As of {when(syncedAt)}</p>{/if}
		{#if stale}<div class="mb-2"><Note>This list may be out of date. Knox could not be reached or the device has not reported recently.</Note></div>{/if}
		<label class="sr-only" for="app-q">Search applications</label>
		<input id="app-q" type="search" class="field mb-2" placeholder="Search name or package" autocomplete="off" bind:value={q} />
		<label class="mb-2 flex min-h-11 items-center gap-3">
			<input type="checkbox" class="toggle toggle-primary" bind:checked={showSystem} />
			<span>Show system apps</span>
		</label>
		<p class="muted py-1 text-[15px]" aria-live="polite">{visible.length} {visible.length === 1 ? 'application' : 'applications'}</p>
		{#each groups as [category, rows] (category)}
			<h3 class="mt-4 mb-1 text-[17px] font-semibold">{category} <span class="muted font-normal">({rows.length})</span></h3>
			<ul>
				{#each rows as a (a.package)}
					{@const reason = uninstallReason(a)}
					<li class="row !items-start">
						<span class="mt-1 flex-none"><AppIcon name={a.name} pkg={a.package} size="md" /></span>
						<span class="rowmain">
							<span class="nm !whitespace-normal break-words">{a.name}</span>
							<span class="sub !whitespace-normal break-all">{a.package}{a.version ? ` · ${a.version}` : ''}</span>
							{#if a.uninstallRequested}<span class="sub">Uninstall requested</span>{/if}
						</span>
						{#if a.protected}<span class="badge flex-none" title={a.protectedReason}>Protected</span>{/if}
						{#if access.canWrite && !a.uninstallRequested && !a.protected}
							<span class="flex-none">
								<button type="button" class="btn btn-quiet-danger" disabled={!!reason} onclick={() => askUninstall(a)} aria-describedby={reason ? `why-${a.package}` : undefined}>Uninstall</button>
								{#if reason}<span id="why-{a.package}" class="sr-only">{reason}</span>{/if}
							</span>
						{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="muted py-4">No applications</p>
		{/each}
		{#if access.canWrite && apps.some((a) => a.protected) && !android}
			<p class="muted mt-3 text-[15px]">Uninstalling is only available for Android devices.</p>
		{/if}
	{/if}
</section>
{/if}

<Dialog bind:open={confirmOpen} title="Uninstall {target?.name ?? 'app'}?">
	<p class="mt-3 text-[16px] break-words">This asks the device to remove {target?.name} ({target?.package}). The app and its data are removed once the device reports back.</p>
	{#if uninstallError}<p class="err" role="alert">{uninstallError}</p>{/if}
	<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => (confirmOpen = false)}>Cancel</button>
		<button type="button" class="btn btn-danger" disabled={busy} onclick={uninstall}>{busy ? 'Requesting...' : 'Uninstall'}</button>
	</div>
</Dialog>
