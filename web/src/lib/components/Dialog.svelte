<script lang="ts">
	import type { Snippet } from 'svelte';

	// Modal sheet (bottom sheet on phones). Children render only while open, so inner state resets.
	let {
		open = $bindable(false),
		title,
		lead,
		children
	}: { open?: boolean; title: string; lead?: Snippet; children: Snippet } = $props();

	let el: HTMLDialogElement;
	const titleId = $props.id();

	$effect(() => {
		if (open && !el.open) el.showModal();
		else if (!open && el.open) el.close();
	});
</script>

<dialog bind:this={el} class="modal modal-bottom sm:modal-middle" aria-labelledby={titleId} onclose={() => (open = false)}>
	<div class="modal-box">
		{#if open}
			{@render lead?.()}
			<h2 id={titleId} class="dlg-title">{title}</h2>
			{@render children()}
		{/if}
	</div>
	<form method="dialog" class="modal-backdrop"><button aria-label="Close">close</button></form>
</dialog>
