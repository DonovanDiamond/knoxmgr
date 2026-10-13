<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import { ApiError } from '#lib/api/client';
	import { createFirstAdmin, verifySetupCode } from '#lib/api/setup';
	import Field from '#components/Field.svelte';
	import HttpsWarning from '#components/HttpsWarning.svelte';
	import KnoxForm from '#components/KnoxForm.svelte';

	let step = $state<1 | 2 | 3 | 4>(1);
	let code = $state('');
	let username = $state('');
	let password = $state('');
	let confirm = $state('');
	let error = $state('');
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);

	// The desktop app passes the code in the fragment (never logged); strip it so it stays out of history.
	onMount(() => {
		const fromHash = new URLSearchParams(location.hash.slice(1)).get('code');
		if (fromHash === null) return;
		code = fromHash;
		replaceState(location.pathname + location.search, {});
	});

	const titles = { 1: 'Welcome', 2: 'Create the administrator', 3: 'Connect Knox', 4: 'All set' };
	const bytes = (s: string) => new TextEncoder().encode(s).length;

	async function verify(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		busy = true;
		try {
			await verifySetupCode(code.trim());
			step = 2;
		} catch (err) {
			if (err instanceof ApiError && err.status === 429) error = 'Too many attempts. Wait a few minutes, then try again.';
			else if (err instanceof ApiError && err.code === 'setup_done') await goto('/login');
			else error = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			busy = false;
		}
	}

	async function createAdmin(e: SubmitEvent) {
		e.preventDefault();
		const v: Record<string, string> = {};
		if (!username.trim()) v.username = 'Enter a username';
		const n = bytes(password);
		if (n < 8 || n > 72) v.password = 'Use 8 to 72 characters';
		if (confirm !== password) v.confirm = 'The passwords do not match';
		errors = v;
		if (Object.keys(v).length) return;
		error = '';
		busy = true;
		try {
			await createFirstAdmin(code.trim(), username.trim(), password);
			step = 3;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'setup_done') await goto('/login');
			else error = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			busy = false;
		}
	}
</script>

<main class="mx-auto max-w-[30rem] px-4 pt-10 pb-10 md:pt-20">
	<p class="text-primary text-[18px] font-extrabold tracking-tight">Knoxmgr setup</p>
	<p class="muted text-[15px]">Step {step} of 4</p>
	<h1 class="big">{titles[step]}</h1>
	<HttpsWarning />

	{#if step === 1}
		<p class="muted mb-5">Enter the setup code from the server log. The desktop app fills it in for you.</p>
		<form class="space-y-5" onsubmit={verify}>
			<Field id="sc" label="Setup code" bind:value={code} />
			{#if error}<div role="alert" class="note warn"><span>{error}</span></div>{/if}
			<button class="btn btn-primary w-full" disabled={busy || !code.trim()}>{busy ? 'Checking...' : 'Continue'}</button>
		</form>
	{:else if step === 2}
		<p class="muted mb-5">This account can manage everything, including other accounts and the Knox connection.</p>
		<form class="space-y-5" onsubmit={createAdmin}>
			<Field id="au" label="Username" bind:value={username} error={errors.username ?? ''} autocomplete="username" />
			<Field id="ap" label="Password" type="password" bind:value={password} error={errors.password ?? ''} hint="8 to 72 characters." autocomplete="new-password" />
			<Field id="ac" label="Confirm password" type="password" bind:value={confirm} error={errors.confirm ?? ''} autocomplete="new-password" />
			{#if error}<div role="alert" class="note warn"><span>{error}</span></div>{/if}
			<button class="btn btn-primary w-full" disabled={busy}>{busy ? 'Creating...' : 'Create administrator'}</button>
		</form>
	{:else if step === 3}
		<p class="muted mb-5">Enter the API credentials of your Knox Manage tenant. You can also do this later in Admin settings.</p>
		<KnoxForm onsaved={() => (step = 4)}>
			<button type="button" class="btn btn-soft2 sm:mr-auto" onclick={() => (step = 4)}>Skip for now</button>
		</KnoxForm>
	{:else}
		<p class="muted mb-5">Setup is complete. You are signed in as {username}.</p>
		<button type="button" class="btn btn-primary w-full" onclick={() => goto('/users')}>Open Knoxmgr</button>
	{/if}
</main>
