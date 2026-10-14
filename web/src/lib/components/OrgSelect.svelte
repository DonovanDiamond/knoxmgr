<script lang="ts">
	import { orgs } from '#lib/orgs.svelte';

	// Organization picker as an indented tree. root limits it to that org and its sub-orgs.
	let {
		id,
		label,
		value = $bindable(''),
		root = null,
		exclude = null,
		allLabel,
		placeholder,
		disabled = false,
		error = ''
	}: {
		id: string;
		label: string;
		value?: string;
		root?: string | null;
		/** Hides this org and its sub-orgs (a parent picker must not offer the org's own subtree). */
		exclude?: string | null;
		/** Adds a leading option with value "" (a filter's "everything"). */
		allLabel?: string;
		/** Adds a disabled leading option with value "" (a form's "choose one"). */
		placeholder?: string;
		disabled?: boolean;
		error?: string;
	} = $props();

	const nodes = $derived.by(() => {
		const hidden = exclude ? orgs.scope(exclude) : null;
		return orgs.tree(root).filter((n) => !hidden?.has(n.org.orgCode));
	});
</script>

<div>
	<label class="lbl" class:sr-only={!!allLabel} for={id}>{label}</label>
	<select
		{id}
		{disabled}
		class="field"
		bind:value
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={error ? `${id}-msg` : undefined}
	>
		{#if allLabel}<option value="">{allLabel}</option>{/if}
		{#if placeholder}<option value="" disabled>{placeholder}</option>{/if}
		{#each nodes as { org, depth } (org.orgCode)}
			<option value={org.orgCode}>{'   '.repeat(depth)}{org.orgName}</option>
		{/each}
	</select>
	{#if error}<p id="{id}-msg" class="err">{error}</p>{/if}
</div>
