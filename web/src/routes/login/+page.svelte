<script lang="ts">
	import { goto } from '$app/navigation';
	import { login } from '#lib/api/auth';
	import { ApiError } from '#lib/api/client';
	import Field from '#components/Field.svelte';
	import HttpsWarning from '#components/HttpsWarning.svelte';
	import Note from '#components/Note.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let directoryDown = $state(false);
	let busy = $state(false);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		directoryDown = false;
		busy = true;
		try {
			await login(username, password);
			await goto('/users');
		} catch (err: unknown) {
			if (err instanceof ApiError && err.code === 'ad_unreachable') directoryDown = true;
			else error = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			busy = false;
		}
	}
</script>

<main class="mx-auto max-w-[26rem] px-4 pt-10 md:pt-24">
	<p class="text-primary text-[18px] font-extrabold tracking-tight">Knoxmgr</p>
	<h1 class="big">Sign in</h1>
	<HttpsWarning />
	{#if directoryDown}
		<div class="mb-6"><Note tone="warn"><strong>Directory unavailable.</strong> Local accounts still work.</Note></div>
	{/if}
	<form class="space-y-5" onsubmit={submit}>
		<Field id="su" label="Username" autocomplete="username" bind:value={username} />
		<Field id="sp" label="Password" type="password" autocomplete="current-password" bind:value={password} />
		{#if error}<div role="alert" class="note warn"><span>{error}</span></div>{/if}
		<button class="btn btn-primary w-full" disabled={busy || !username || !password}>
			{busy ? 'Signing in...' : 'Sign in'}
		</button>
	</form>
</main>
