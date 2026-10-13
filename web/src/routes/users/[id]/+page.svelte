<script lang="ts">
	import { page } from '$app/state';
	import UserDetail from '#components/UserDetail.svelte';
	import UserEditor from '#components/UserEditor.svelte';

	let editing = $state(false);
</script>

{#key page.params.id}
	{#if editing}
		<UserEditor id={page.params.id} oncancel={() => (editing = false)} ondone={() => (editing = false)} />
	{:else}
		<UserDetail id={page.params.id ?? ''} onedit={() => (editing = true)} />
	{/if}
{/key}
