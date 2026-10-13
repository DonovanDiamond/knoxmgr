<script lang="ts">
	import { onMount } from 'svelte';
	import { getUpdateStatus } from '#lib/api/settings';
	import type { UpdateStatus } from '#lib/api/types';
	import Icon from './Icon.svelte';

	const KEY = 'knoxmgr.dismissedUpdate';

	let status = $state<UpdateStatus | null>(null);
	let dismissed = $state('');

	onMount(async () => {
		try {
			dismissed = localStorage.getItem(KEY) ?? '';
		} catch {
			// Storage blocked: the notice just cannot be remembered as dismissed.
		}
		try {
			status = await getUpdateStatus();
		} catch {
			// The notice is optional; stay silent on failure.
		}
	});

	// Only http(s) links, so a bad feed value can never become a javascript: URL.
	const safeUrl = $derived(status?.url && /^https?:\/\//i.test(status.url) ? status.url : '');
	const show = $derived(!!status?.enabled && status.updateAvailable && !!status.latest && status.latest !== dismissed);

	function dismiss() {
		if (!status?.latest) return;
		dismissed = status.latest;
		try {
			localStorage.setItem(KEY, dismissed);
		} catch {
			// Dismissal lasts until reload.
		}
	}
</script>

{#if show && status}
	<div class="note" role="status">
		<Icon name="info" size={18} />
		<span class="flex-1">
			Update available:
			{#if safeUrl}
				<a href={safeUrl} target="_blank" rel="noopener noreferrer" class="link font-semibold">{status.latest}</a>
			{:else}
				<strong>{status.latest}</strong>
			{/if}
		</span>
		<button type="button" class="btn btn-ghost btn-sm" onclick={dismiss} aria-label="Dismiss update notice">Dismiss</button>
	</div>
{/if}

<style>
	/* The shared .note top-aligns its icon; this banner is a single centred row. */
	.note {
		align-items: center;
	}
	.note :global(svg) {
		margin-top: 0;
	}
</style>
