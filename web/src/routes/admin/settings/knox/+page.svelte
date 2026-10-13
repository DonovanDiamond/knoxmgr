<script lang="ts">
	import { session } from '#lib/api/session.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import KnoxForm from '#components/KnoxForm.svelte';
	import Note from '#components/Note.svelte';
</script>

<BigTitle title="Knox connection" back="/admin" />

{#if session.user?.role !== 'admin'}
	<Note tone="warn" icon="lock"><strong>Admins only.</strong> Settings need an administrator account.</Note>
{:else}
	<div class="max-w-xl"><KnoxForm /></div>
{/if}
