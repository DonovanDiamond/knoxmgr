<script lang="ts" generics="T">
	// Type-to-filter list with keyboard selection (combobox pattern).
	let {
		items,
		label,
		onpick,
		placeholder = 'Search',
		disabled = false,
		busyKey = '',
		key = label,
		busyText = label
	}: {
		items: T[];
		label: (item: T) => string;
		onpick: (item: T) => void;
		placeholder?: string;
		disabled?: boolean;
		busyKey?: string; // key of the row whose action is in flight; the other rows dim
		key?: (item: T) => string;
		busyText?: (item: T) => string;
	} = $props();

	const uid = $props.id();
	let q = $state('');
	let active = $state(0);

	const shown = $derived(items.filter((i) => label(i).toLowerCase().includes(q.trim().toLowerCase())));
	const open = $derived(q.trim() !== '' || shown.length > 0);
	const current = $derived(Math.min(active, Math.max(shown.length - 1, 0)));

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			active = Math.min(current + 1, shown.length - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			active = Math.max(current - 1, 0);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			if (shown[current]) onpick(shown[current]);
		} else if (e.key === 'Escape' && q) {
			// Only swallow Escape while there is text to clear, so a parent dialog can still close.
			e.stopPropagation();
			q = '';
		}
	}
</script>

<input
	type="text"
	class="input input-bordered w-full"
	role="combobox"
	aria-expanded={open}
	aria-controls="{uid}-list"
	aria-autocomplete="list"
	aria-activedescendant={shown.length ? `${uid}-${current}` : undefined}
	aria-label={placeholder}
	{placeholder}
	{disabled}
	bind:value={q}
	oninput={() => (active = 0)}
	{onkeydown}
/>
<ul id="{uid}-list" role="listbox" aria-label={placeholder} class="mt-2 max-h-[40vh] overflow-y-auto px-3">
	{#each shown as item, i (i)}
		<!-- Options are not focusable by design; keyboard goes through the combobox input. -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<li
			id="{uid}-{i}"
			role="option"
			aria-selected={i === current}
			class="row row-hover cursor-pointer {i === current ? 'bg-base-200' : ''} {busyKey && (key(item) === busyKey ? 'relative' : 'opacity-40')}"
			onclick={() => !disabled && onpick(item)}
		>
			{#if busyKey && key(item) === busyKey}<span class="absolute inset-0 animate-pulse rounded-[inherit] bg-primary/20" aria-hidden="true"></span><span class="loading loading-spinner loading-md text-primary relative mr-3" role="status" aria-label="In progress"></span>{/if}
			<span class="rowmain relative"><span class="nm !text-[16px]">{busyKey && key(item) === busyKey ? busyText(item) : label(item)}</span></span>
		</li>
	{:else}
		<li class="muted py-8 text-center" role="presentation">No matches.</li>
	{/each}
</ul>
