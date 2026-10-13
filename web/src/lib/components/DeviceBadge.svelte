<script lang="ts">
	import type { Freshness } from '#lib/api/devices';

	let { count, freshness, title }: { count?: number; freshness: Freshness; title?: string } = $props();

	const tone: Record<Freshness, string> = {
		none: '',
		fresh: 'badge-success',
		stale: 'badge-warning',
		old: 'badge-error',
		never: 'badge-neutral badge-soft',
		not_enrolled: 'badge-neutral badge-soft'
	};

	const text: Record<Freshness, string> = {
		none: 'Never connected',
		fresh: 'Active',
		stale: 'Stale',
		old: 'Inactive',
		never: 'Never',
		not_enrolled: 'Not enrolled'
	};
</script>

<span class="badge {tone[freshness]}" {title}>{count ?? text[freshness]}</span>
