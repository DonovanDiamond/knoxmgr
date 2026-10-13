<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { getDeviceLocation, type DeviceLocation, type GeoPoint } from '#lib/api/devices';
	import Note from './Note.svelte';

	let { id }: { id: string } = $props();

	let days = $state<1 | 7 | 30>(7);
	let loc = $state<DeviceLocation | null>(null);
	let loading = $state(true);
	let error = $state('');

	$effect(() => {
		const deviceId = id;
		const period = days;
		loading = true;
		error = '';
		getDeviceLocation(deviceId, period)
			.then((r) => {
				loc = r;
			})
			.catch((e) => {
				error = e instanceof ApiError ? e.message : 'Could not reach the server.';
			})
			.finally(() => {
				loading = false;
			});
	});

	let mapEl = $state<HTMLDivElement>();
	const points = $derived(
		(loc?.history ?? []).filter(
			(p) => Number.isFinite(p.latitude) && Number.isFinite(p.longitude) && Math.abs(p.latitude) <= 90 && Math.abs(p.longitude) <= 180
		)
	);
	const showMap = $derived(!!loc?.showMap && points.length > 0 && !error);

	// Rebuilt whenever the points or the container change; cleanup destroys the map.
	$effect(() => {
		const el = mapEl;
		const pts = points;
		if (!el || !pts.length) return;
		let map: import('leaflet').Map | undefined;
		let cancelled = false;
		(async () => {
			const L = (await import('leaflet')).default;
			await import('leaflet/dist/leaflet.css');
			if (cancelled) return;
			map = L.map(el, { scrollWheelZoom: false });
			L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
				maxZoom: 19,
				attribution: '<a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener noreferrer">© OpenStreetMap contributors</a>'
			}).addTo(map);
			const latlngs = pts.map((p): [number, number] => [p.latitude, p.longitude]);
			if (latlngs.length > 1) L.polyline(latlngs, { color: '#2563eb', weight: 3, opacity: 0.7 }).addTo(map);
			for (const ll of latlngs) L.circleMarker(ll, { radius: 4, color: '#2563eb', fillColor: '#2563eb', fillOpacity: 1, weight: 1 }).addTo(map);
			// The latest point is drawn last so it sits on top.
			const latest = pts.reduce((a, b) => (new Date(b.time) > new Date(a.time) ? b : a));
			L.circleMarker([latest.latitude, latest.longitude], { radius: 9, color: '#fff', fillColor: '#dc2626', fillOpacity: 1, weight: 3 }).addTo(map);
			map.fitBounds(L.latLngBounds(latlngs), { padding: [24, 24], maxZoom: 16 });
		})();
		return () => {
			cancelled = true;
			map?.remove();
		};
	});

	// Only build a link from coordinates that are real and in range.
	function mapUrl(p: GeoPoint): string | null {
		const { latitude: lat, longitude: lon } = p;
		if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) return null;
		return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lon}`;
	}
	const when = (iso: string) => new Date(iso).toLocaleString();
</script>

{#snippet coords(p: GeoPoint)}
	{@const url = mapUrl(p)}
	{#if url}
		<a href={url} target="_blank" rel="noopener noreferrer" class="link">{p.latitude}, {p.longitude}</a>
	{:else}
		{p.latitude}, {p.longitude}
	{/if}
{/snippet}

<section class="mt-8">
	<h2 class="mb-2 text-[20px] font-semibold">Location</h2>
	{#if error}
		<Note tone="warn">{error}</Note>
	{:else if loc}
		{#if loc.cached}<div class="mb-2"><Note>Knox unreachable — showing data as of {when(loc.fetchedAt)}</Note></div>{/if}
		<div class="mb-3 rounded-lg border border-base-300 p-3">
			<p class="muted text-[15px]">Current location</p>
			<p class="text-[20px] font-semibold">
				{#if loc.current}{@render coords(loc.current)}{:else}Not available{/if}
			</p>
			{#if loc.current}<p class="muted text-[15px]">{when(loc.current.time)}</p>{/if}
		</div>
	{/if}
	<label class="muted text-[15px]" for="loc-days">History period</label>
	<select id="loc-days" class="field mb-2" bind:value={days}>
		<option value={1}>Last day</option>
		<option value={7}>Last 7 days</option>
		<option value={30}>Last 30 days</option>
	</select>
	{#if loading}
		<div><span class="loading loading-spinner"></span></div>
	{:else if loc && !error}
		{#if showMap}
			<div bind:this={mapEl} class="relative z-0 mb-3 h-64 w-full overflow-hidden rounded-lg border border-base-300 sm:h-96" role="img" aria-label="Map of device location history"></div>
		{/if}
		<div class="max-h-[30rem] overflow-auto">
			<table class="table table-pin-rows">
				<thead><tr><th>Time</th><th>Latitude, longitude</th><th>Altitude</th></tr></thead>
				<tbody>
					{#each loc.history as p}
						<tr><td>{when(p.time)}</td><td>{@render coords(p)}</td><td>{p.altitude ?? '-'}</td></tr>
					{:else}
						<tr><td colspan="3" class="muted">No locations in this period</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>
