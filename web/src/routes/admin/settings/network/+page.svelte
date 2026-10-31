<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { getProxySettings, saveProxySettings } from '#lib/api/settings';
	import { session } from '#lib/api/session.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Field from '#components/Field.svelte';
	import Note from '#components/Note.svelte';
	import type { ProxySettings } from '#lib/api/types';
	import { toast } from '#lib/toast.svelte';

	const isAdmin = $derived(session.user?.role === 'admin');

	let loading = $state(true);
	let loadError = $state('');
	let current = $state<ProxySettings | null>(null);
	let url = $state('');
	let username = $state('');
	let password = $state('');
	let clearPassword = $state(false);
	let bypass = $state('');
	let formError = $state('');
	let saving = $state(false);

	function apply(s: ProxySettings) {
		current = s;
		url = s.url;
		username = s.username;
		bypass = s.bypass;
		password = '';
		clearPassword = false;
	}

	async function load() {
		loading = true;
		loadError = '';
		try {
			apply(await getProxySettings());
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		if (isAdmin) load();
	});

	const inEffect = $derived(
		!current || current.source === 'none'
			? 'Direct (no proxy)'
			: current.source === 'env'
				? `From environment (HTTPS_PROXY): ${current.effectiveUrl}`
				: `Proxy setting: ${current.effectiveUrl}`
	);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		formError = '';
		try {
			apply(await saveProxySettings({ url: url.trim(), username: username.trim(), password, clearPassword, bypass: bypass.trim() }));
			toast.show('Settings saved');
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			saving = false;
		}
	}
</script>

<BigTitle title="Network" back="/admin" />

{#if !isAdmin}
	<Note tone="warn" icon="lock"><strong>Admins only.</strong> Settings need an administrator account.</Note>
{:else if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load settings</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<form class="flex max-w-xl flex-col gap-4" onsubmit={save}>
		<p class="muted">
			Outbound proxy for Knox and update checks. It does not apply to Active Directory. Knox "Test connection" uses it.
		</p>
		<p role="status"><strong>In effect:</strong> {inEffect}</p>
		<div>
			<label class="lbl" for="n-url">Proxy URL</label>
			<input id="n-url" class="field" type="text" autocomplete="off" placeholder="http://proxy.example:3128" aria-describedby="n-url-msg" bind:value={url} />
			<p id="n-url-msg" class="hint">http or https only. Leave empty for a direct connection.</p>
		</div>
		<Field id="n-user" label="Username" bind:value={username} />
		<div>
			<label class="lbl" for="n-pass">Password</label>
			<input
				id="n-pass"
				class="field"
				type="password"
				autocomplete="new-password"
				placeholder={current?.passwordSet ? 'unchanged' : ''}
				disabled={clearPassword}
				aria-describedby="n-pass-msg"
				bind:value={password}
			/>
			<p id="n-pass-msg" class="hint">Never shown. Leave empty to keep the stored password.</p>
		</div>
		{#if current?.passwordSet}
			<label class="flex min-h-11 items-center gap-3">
				<input type="checkbox" class="checkbox checkbox-primary" bind:checked={clearPassword} onchange={() => (password = '')} />
				<span>Clear stored password</span>
			</label>
		{/if}
		<Field id="n-bypass" label="Bypass list" bind:value={bypass} hint="Comma-separated hosts, .domains, IPs or CIDRs that skip the proxy." />
		{#if formError}<p class="err" role="alert">{formError}</p>{/if}
		<div class="mt-2 flex sm:justify-end">
			<button type="submit" class="btn btn-primary w-full sm:w-auto" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
		</div>
	</form>
{/if}
