<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import {
		checkForUpdates,
		getGeneralSettings,
		getSyncState,
		getUpdateStatus,
		saveGeneralSettings,
		syncNow,
		type SyncRun
	} from '#lib/api/settings';
	import { session } from '#lib/api/session.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Field from '#components/Field.svelte';
	import Note from '#components/Note.svelte';
	import type { UpdateStatus } from '#lib/api/types';
	import { toast } from '#lib/toast.svelte';

	const isAdmin = $derived(session.user?.role === 'admin');

	let loading = $state(true);
	let loadError = $state('');
	let installationName = $state('');
	let interval = $state('15');
	let checkUpdates = $state(true);
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let saving = $state(false);

	async function load() {
		loading = true;
		loadError = '';
		try {
			const s = await getGeneralSettings();
			installationName = s.installationName;
			interval = String(s.syncIntervalMinutes);
			checkUpdates = s.checkUpdates;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		if (isAdmin) load();
	});

	let lastRun = $state<SyncRun | null>(null);
	let running = $state(false);
	let syncError = $state('');
	let starting = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function refreshSync() {
		clearTimeout(timer);
		try {
			const st = await getSyncState();
			lastRun = st.runs[0] ?? null;
			running = st.running;
			syncError = '';
		} catch (e) {
			syncError = e instanceof ApiError ? e.message : 'Could not reach the server.';
			running = false;
		}
		if (running) timer = setTimeout(refreshSync, 3000);
	}
	$effect(() => {
		if (isAdmin) refreshSync();
		return () => clearTimeout(timer);
	});

	async function startSync() {
		starting = true;
		syncError = '';
		try {
			await syncNow();
		} catch (e) {
			syncError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			starting = false;
		}
		await refreshSync();
	}

	let update = $state<UpdateStatus | null>(null);
	let updateError = $state('');
	let checking = $state(false);
	$effect(() => {
		if (!isAdmin) return;
		getUpdateStatus().then(
			(u) => (update = u),
			(e) => (updateError = e instanceof ApiError ? e.message : 'Could not reach the server.')
		);
	});

	async function checkNow() {
		checking = true;
		updateError = '';
		try {
			update = await checkForUpdates();
		} catch (e) {
			updateError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			checking = false;
		}
	}

	const when = (iso: string) => new Date(iso).toLocaleString();

	async function save(e: SubmitEvent) {
		e.preventDefault();
		const minutes = Number(interval);
		const v: Record<string, string> = {};
		if (!installationName.trim()) v.name = 'Enter a name';
		if (!Number.isInteger(minutes) || minutes < 1 || minutes > 1440) v.interval = 'Enter a whole number from 1 to 1440';
		errors = v;
		if (Object.keys(v).length) return;
		saving = true;
		formError = '';
		try {
			const s = await saveGeneralSettings({ installationName: installationName.trim(), syncIntervalMinutes: minutes, checkUpdates });
			installationName = s.installationName;
			interval = String(s.syncIntervalMinutes);
			checkUpdates = s.checkUpdates;
			void getUpdateStatus().then((u) => (update = u), () => {});
			toast.show('Settings saved');
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			saving = false;
		}
	}
</script>

<BigTitle title="General" back="/admin" />

{#if !isAdmin}
	<Note tone="warn" icon="lock"><strong>Admins only.</strong> Settings need an administrator account.</Note>
{:else if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load settings</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<form class="flex max-w-xl flex-col gap-4" onsubmit={save}>
		<Field id="g-name" label="Installation name" bind:value={installationName} error={errors.name ?? ''} />
		<Field
			id="g-interval"
			label="Sync interval (minutes)"
			bind:value={interval}
			inputmode="numeric"
			error={errors.interval ?? ''}
			hint="1 to 1440. Used by background sync."
		/>
		<label class="flex min-h-11 items-center gap-3">
			<input type="checkbox" class="toggle toggle-primary" bind:checked={checkUpdates} />
			<span>Check for updates</span>
		</label>
		{#if formError}<p class="err" role="alert">{formError}</p>{/if}
		<div class="mt-2 flex sm:justify-end">
			<button type="submit" class="btn btn-primary w-full sm:w-auto" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
		</div>
	</form>

	<section class="mt-10 flex max-w-xl flex-col gap-3">
		<h2 class="text-[20px] font-semibold">Knox sync</h2>
		{#if running}
			<p class="muted" role="status">Sync is running...</p>
		{:else if lastRun}
			<p>
				Last sync {when(lastRun.finishedAt ?? lastRun.startedAt)}:
				<strong class={lastRun.status === 'ok' ? 'text-success' : ''}>{lastRun.status === 'ok' ? 'succeeded' : lastRun.status}</strong>
			</p>
			<p class="muted">
				{lastRun.counts.orgs} orgs, {lastRun.counts.users} users, {lastRun.counts.groups} groups,
				{lastRun.counts.memberships} memberships, {lastRun.counts.devices} devices
			</p>
			{#if lastRun.status === 'failed' && lastRun.error}<p class="err" role="alert">{lastRun.error}</p>{/if}
		{:else}
			<p class="muted">No sync has run yet.</p>
		{/if}
		{#if syncError}<p class="err" role="alert">{syncError}</p>{/if}
		<div><button type="button" class="btn btn-soft2" disabled={starting || running} onclick={startSync}>{running ? 'Syncing...' : 'Sync now'}</button></div>
	</section>

	<section class="mt-10 flex max-w-xl flex-col gap-3">
		<h2 class="text-[20px] font-semibold">Updates</h2>
		{#if update}
			<p>Current version: <strong>{update.current}</strong></p>
			{#if update.latest}
				<p>Latest seen: <strong>{update.latest}</strong>{update.updateAvailable ? ' (update available)' : ''}</p>
			{/if}
			<p class="muted">Last checked: {update.checkedAt ? when(update.checkedAt) : 'never'}</p>
			{#if update.error}<p class="err" role="alert">{update.error}</p>{/if}
		{/if}
		{#if updateError}<p class="err" role="alert">{updateError}</p>{/if}
		<div>
			<button type="button" class="btn btn-soft2" disabled={checking || !checkUpdates} onclick={checkNow}>{checking ? 'Checking...' : 'Check now'}</button>
		</div>
	</section>
{/if}
