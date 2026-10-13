<script lang="ts">
	import { session } from '#lib/api/session.svelte';
</script>

<div class="py-14 text-center">
	<p class="text-[20px] font-semibold">Knox is not connected</p>
	{#if session.user?.role === 'admin'}
		<p class="muted mt-1">Add your Knox credentials to see people, devices and groups here.</p>
		<a href="/admin/settings/knox" class="btn btn-primary mt-5">Connect Knox</a>
	{:else}
		<p class="muted mt-1">Ask an administrator to connect Knox.</p>
	{/if}
</div>
