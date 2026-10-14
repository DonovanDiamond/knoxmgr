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

	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		const rows = apps.filter(
			(a) =>
				(showSystem || !a.system) &&
				(!category || (a.category || 'Other') === category) &&
				(!needle || `${a.name} ${a.alternateNames.join(' ')} ${a.package}`.toLowerCase().includes(needle))
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

	const appTitle = (a: AppSummary) => [a.name, ...a.alternateNames].join(' / ');

	const cols: [SortKey, string][] = [
		['name', 'Name'],
		['package', 'Package'],
		['deviceCount', 'Devices']
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
		<div class="muted flex items-center gap-2 text-[14px]">
			<span class="mr-1">Sort by</span>
			{#each cols as [k, label] (k)}
				<button type="button" class="min-h-11 px-2 font-semibold" onclick={() => sortBy(k)}>
					{label}{sortKey === k ? (sortDir === 1 ? ' ▲' : ' ▼') : ''}
				</button>
			{/each}
		</div>
		{#each groups as [cat, rows] (cat)}
			<h2 class="pt-4 pb-1 text-[18px] font-bold">{cat}</h2>
			<ul>
				{#each rows as a (a.package)}
					<li class="row row-hover">
						<span class="flex-none"><AppIcon name={a.name} pkg={a.package} /></span>
						<span class="rowmain">
							<a href="/apps/{encodeURIComponent(a.package)}" class="nm truncate after:absolute after:inset-0" title={appTitle(a)}>{a.name}{#if a.alternateNames.length}{' '}<span class="muted text-[14px] font-normal">({a.alternateNames.join(', ')})</span>{/if}</a>
							<span class="sub" title={a.package}>{a.package}</span>
						</span>
						{#if a.system}<span class="badge flex-none">System</span>{/if}
						{#if a.protected}<span class="badge flex-none" title={a.protectedReason}>Protected</span>{/if}
						<span class="w-12 flex-none text-right font-semibold">{a.deviceCount}</span>
					</li>
				{/each}
			</ul>
		{/each}
	{:else}
		<div class="py-14 text-center">
			<p class="text-[20px] font-semibold">{apps.length ? 'No app matches' : 'No apps yet'}</p>
			<p class="muted mt-1">{apps.length ? 'Check the spelling, or change the filters.' : 'Apps installed on synced devices will appear here.'}</p>
		</div>
	{/if}
{/if}
