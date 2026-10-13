<script lang="ts">
	// Initials circle. With onclick it is a select toggle (aria-pressed); without, a plain badge.
	let {
		name,
		seed = name,
		selected = false,
		disabled = false,
		small = false,
		onclick
	}: {
		name: string;
		/** Picks the colour tone; defaults to the name. */
		seed?: string;
		selected?: boolean;
		disabled?: boolean;
		small?: boolean;
		onclick?: () => void;
	} = $props();

	const initials = $derived(
		name
			.split(/[\s.]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((w) => w[0])
			.join('')
			.toUpperCase() || '?'
	);
	const tone = $derived(['', 't1', 't2'][[...seed].reduce((n, c) => n + c.charCodeAt(0), 0) % 3]);
</script>

{#if onclick}
	<button
		type="button"
		class="av {tone}"
		class:sm={small}
		{disabled}
		{onclick}
		aria-pressed={selected}
		aria-label="{selected ? 'Deselect' : 'Select'} {name}"
		title="{selected ? 'Deselect' : 'Select'} {name}"
	>
		<span class="av-init">{initials}</span>
		<svg class="av-ic av-check" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7" /></svg>
		<svg class="av-ic av-plus" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
		<svg class="av-ic av-minus" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12h14" /></svg>
	</button>
{:else}
	<span class="av {tone}" class:sm={small} aria-hidden="true">{initials}</span>
{/if}
