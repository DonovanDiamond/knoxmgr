<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { compareCategories, listAppCategories, listApps, type AppCategory, type AppSummary } from '#lib/api/apps';
	import AppIcon from '#components/AppIcon.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Icon from '#components/Icon.svelte';
	import KnoxOffline from '#components/KnoxOffline.svelte';
	import { knoxStatus } from '#lib/health.svelte';

	let apps = $state<AppSummary[]>([]);
	let categories = $state<AppCategory[]>([]);
	let loading = $state(true);
	let loadError = $state('');
	let q = $state('');
	let category = $state('');
	let showSystem = $state(false);

	type SortKey = 'name' | 'package' | 'deviceCount';
	let sortKey = $state<SortKey>('name');
	let sortDir = $state<1 | -1>(1);

	async function load() {
		loading = true;
		loadError = '';
		try {
			const [a, c] = await Promise.all([listApps({ system: showSystem }), listAppCategories()]);
			apps = a.apps;
			categories = c.categories;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		showSystem; // reload when the toggle changes
		load();
	});

	function sortBy(k: SortKey) {
		if (sortKey === k) sortDir = sortDir === 1 ? -1 : 1;
		else {
			sortKey = k;
			sortDir = k === 'deviceCount' ? -1 : 1;
		}
	}
	const ariaSort = (k: SortKey) => (sortKey === k ? (sortDir === 1 ? 'ascending' : 'descending') : 'none');

	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		const rows = apps.filter(
			(a) =>
				(showSystem || !a.system) &&
				(!category || (a.category || 'Other') === category) &&
				(!needle || `${a.name} ${a.package}`.toLowerCase().includes(needle))
		);
		return rows.sort((a, b) => {
			const x = a[sortKey];
			const y = b[sortKey];
			const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y));
			return c * sortDir || a.name.localeCompare(b.name);
		});
	});

	const groups = $derived.by(() => {
		const m = new Map<string, AppSummary[]>();
		for (const a of visible) {
			const k = a.category || 'Other';
			m.set(k, [...(m.get(k) ?? []), a]);
		}
		return [...m.entries()].sort(([a], [b]) => compareCategories(a, b));
	});

	const cols: [SortKey, string, string][] = [
		['name', 'Name', ''],
		['package', 'Package', 'hidden md:table-cell'],
		['deviceCount', 'Devices', 'w-24 text-right']
	];
</script>

<BigTitle title="Apps" />

<div class="mb-3 flex flex-wrap items-center gap-2">
	<div class="relative min-w-[16rem] flex-1">
		<span class="muted absolute top-1/2 left-3 -translate-y-1/2"><Icon name="search" size={20} /></span>
		<label class="sr-only" for="q">Search apps</label>
		<input id="q" type="search" class="field !pl-10" placeholder="Search name or package" autocomplete="off" bind:value={q} />
	</div>
	<label class="sr-only" for="cat">Category</label>
	<select id="cat" class="field !w-auto" bind:value={category}>
		<option value="">All categories</option>
		{#each categories as c (c.id)}<option value={c.name}>{c.name}</option>{/each}
	</select>
	<label class="flex min-h-11 items-center gap-2">
		<input type="checkbox" class="checkbox checkbox-primary" bind:checked={showSystem} />
		<span>Show system apps</span>
	</label>
</div>

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if knoxStatus.configured === false}
	<KnoxOffline />
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load apps</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<p class="muted py-2 text-[15px]" aria-live="polite">{visible.length} {visible.length === 1 ? 'app' : 'apps'}{q ? ' match' : ''}</p>
	{#if groups.length}
		<table class="w-full table-fixed text-left">
			<thead>
				<tr class="muted text-[14px]">
					<th class="w-14"><span class="sr-only">Icon</span></th>
					{#each cols as [k, label, cls] (k)}
						<th class={cls} aria-sort={ariaSort(k)}>
							<button type="button" class="min-h-11 font-semibold" onclick={() => sortBy(k)}>
								{label}{sortKey === k ? (sortDir === 1 ? ' ▲' : ' ▼') : ''}
							</button>
						</th>
					{/each}
				</tr>
			</thead>
			{#each groups as [cat, rows] (cat)}
				<tbody>
					<tr><th colspan="4" scope="colgroup" class="pt-4 pb-1 text-[18px] font-bold">{cat}</th></tr>
					{#each rows as a (a.package)}
						<tr class="row-hover relative border-t" style="border-color: var(--line)">
							<td class="py-2 pr-3"><AppIcon name={a.name} pkg={a.package} /></td>
							<td class="py-2 pr-3">
								<a href="/apps/{encodeURIComponent(a.package)}" class="font-semibold after:absolute after:inset-0">{a.name}</a>
								{#if a.protected}<span class="badge ml-2 align-middle" title={a.protectedReason}>Protected</span>{/if}
								<span class="muted block text-[14px] break-all md:hidden">{a.package}</span>
							</td>
							<td class="muted hidden py-2 pr-3 text-[14px] break-all md:table-cell">{a.package}</td>
							<td class="py-2 text-right font-semibold">{a.deviceCount}</td>
						</tr>
					{/each}
				</tbody>
			{/each}
		</table>
	{:else}
		<div class="py-14 text-center">
			<p class="text-[20px] font-semibold">{apps.length ? 'No app matches' : 'No apps yet'}</p>
			<p class="muted mt-1">{apps.length ? 'Check the spelling, or change the filters.' : 'Apps installed on synced devices will appear here.'}</p>
		</div>
	{/if}
{/if}
