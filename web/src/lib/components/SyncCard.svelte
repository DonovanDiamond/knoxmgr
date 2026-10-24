<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { getSyncState, syncNow, type SyncState } from '#lib/api/settings';
	import { toast } from '#lib/toast.svelte';

	// Called once a sync that this card watched has finished, so the page can reload its data.
	let { onfinished }: { onfinished: () => void } = $props();

	let sync = $state<SyncState | null>(null);
	let error = $state('');
	let now = $state(Date.now());

	const last = $derived(sync?.runs.find((r) => r.status !== 'running'));

	function ago(iso: string): string {
		const min = Math.max(0, Math.round((now - Date.parse(iso)) / 60000));
		if (min < 1) return 'just now';
		if (min < 60) return `${min} min ago`;
		const h = Math.round(min / 60);
		if (h < 48) return `${h} h ago`;
		return `${Math.round(h / 24)} days ago`;
	}

	async function refresh() {
		try {
			sync = await getSyncState();
			now = Date.now();
			error = '';
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not reach the server.';
		}
	}

	// Re-poll until the run ends; the timer is cleared with the component.
	let timer: ReturnType<typeof setTimeout> | undefined;
	let watching = false;
	async function watch() {
		watching = true;
		await refresh();
		if (sync?.running) {
			timer = setTimeout(watch, 2000);
			return;
		}
		watching = false;
		onfinished();
	}

	async function start() {
		try {
			await syncNow();
			void watch();
		} catch (e) {
			toast.show(e instanceof ApiError ? e.message : 'Could not start the sync');
		}
	}

	$effect(() => {
		refresh().then(() => {
			if (sync?.running && !watching) void watch();
		});
		return () => clearTimeout(timer);
	});
</script>

<h2 class="text-[20px] font-bold">Sync</h2>
<div class="panel mt-2 flex flex-wrap items-center justify-between gap-3">
	<div>
		{#if error}
			<p class="muted">{error}</p>
		{:else if !sync}
			<span class="loading loading-spinner loading-sm"></span>
		{:else if sync.running}
			<p class="flex items-center gap-2 font-semibold"><span class="loading loading-spinner loading-sm"></span> Syncing…</p>
		{:else if last}
			<p class="font-semibold">
				{last.status === 'ok' ? 'Last sync succeeded' : 'Last sync failed'}
				<span class="badge {last.status === 'ok' ? 'badge-success' : 'badge-error'} ml-1">{last.status === 'ok' ? 'OK' : 'Failed'}</span>
			</p>
			<p class="muted">{ago(last.finishedAt ?? last.startedAt)}{last.status === 'failed' && last.error ? ` · ${last.error}` : ''}</p>
		{:else}
			<p class="font-semibold">Not synced yet</p>
			<p class="muted">Run a sync to copy your Knox data here.</p>
		{/if}
	</div>
	<button type="button" class="btn btn-primary" onclick={start} disabled={!sync || sync.running}>Sync now</button>
</div>
