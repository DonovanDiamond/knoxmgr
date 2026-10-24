<script lang="ts">
	import type { Device } from '#lib/api/devices';

	let { devices }: { devices: Device[] } = $props();

	// "none" and "never" both mean no connection on record.
	const buckets = $derived([
		{ label: 'Within 24 hours', color: 'bg-success', n: devices.filter((d) => d.freshness === 'fresh').length },
		{ label: '1–7 days', color: 'bg-warning', n: devices.filter((d) => d.freshness === 'stale').length },
		{ label: 'Over 7 days', color: 'bg-error', n: devices.filter((d) => d.freshness === 'old').length },
		{ label: 'Never', color: 'bg-neutral', n: devices.filter((d) => d.freshness === 'never' || d.freshness === 'none').length }
	]);
	const total = $derived(buckets.reduce((s, b) => s + b.n, 0));
</script>

<h2 class="text-[20px] font-bold">Device health</h2>
{#if total === 0}
	<p class="muted mt-2">No enrolled devices yet.</p>
{:else}
	<div class="bg-base-200 mt-3 flex h-3 overflow-hidden rounded-full" role="img" aria-label="Enrolled devices by last connection">
		{#each buckets as b (b.label)}
			{#if b.n}<div class={b.color} style="width: {(b.n / total) * 100}%" title="{b.label}: {b.n}"></div>{/if}
		{/each}
	</div>
	<ul class="mt-3 flex flex-wrap gap-x-5 gap-y-1">
		{#each buckets as b (b.label)}
			<li class="flex items-center gap-2">
				<span class="inline-block h-3 w-3 rounded-full {b.color}"></span>
				<span class="muted">{b.label}</span>
				<strong>{b.n}</strong>
			</li>
		{/each}
	</ul>
{/if}
