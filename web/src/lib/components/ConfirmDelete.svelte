<script lang="ts">
	import type { Snippet } from 'svelte';
	import { ApiError } from '#lib/api/client';
	import Dialog from './Dialog.svelte';

	// Destructive action gated by typing confirmKey. onconfirm runs once; a throw is shown inline.
	let {
		open = $bindable(false),
		title,
		confirmKey,
		actionLabel,
		onconfirm,
		children
	}: {
		open?: boolean;
		title: string;
		/** The exact text the user must type (user id, group name). */
		confirmKey: string;
		actionLabel: string;
		onconfirm: () => Promise<void>;
		children: Snippet;
	} = $props();

	let typed = $state('');
	let busy = $state(false);
	let error = $state('');

	$effect(() => {
		if (open) {
			typed = '';
			error = '';
		}
	});

	async function run() {
		busy = true;
		error = '';
		try {
			await onconfirm();
			open = false;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open {title}>
	<p class="mt-3 text-[16px]">{@render children()}</p>
	<div class="mt-5">
		<label class="lbl" for="confirm-key">Type <strong>{confirmKey}</strong> to confirm</label>
		<input
			id="confirm-key"
			class="field"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			bind:value={typed}
		/>
	</div>
	{#if error}<p class="err" role="alert">{error}</p>{/if}
	<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => (open = false)}>Cancel</button>
		<button type="button" class="btn btn-danger" disabled={busy || typed.trim() !== confirmKey} onclick={run}>
			{actionLabel}
		</button>
	</div>
</Dialog>
