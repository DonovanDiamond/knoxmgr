<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { getKnoxSettings, saveKnoxSettings, testKnoxSettings } from '#lib/api/settings';
	import type { KnoxSettings, KnoxSettingsInput, KnoxTestResult } from '#lib/api/types';
	import { knoxStatus } from '#lib/health.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { toast } from '#lib/toast.svelte';
	import Field from './Field.svelte';
	import Note from './Note.svelte';

	// Knox connection form, shared by the setup wizard and admin settings.
	let { onsaved, children }: { onsaved?: () => void; children?: import('svelte').Snippet } = $props();

	let loaded = $state<KnoxSettings | null>(null);
	let loadError = $state('');
	let region = $state('');
	let baseURL = $state('');
	let clientId = $state('');
	let tenantId = $state('');
	let clientSecret = $state('');
	let allowInsecureHTTP = $state(false);

	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let result = $state<KnoxTestResult | null>(null);
	let testing = $state(false);
	let saving = $state(false);

	async function load() {
		loadError = '';
		try {
			const s = await getKnoxSettings();
			loaded = s;
			region = s.region || s.regions[0]?.id || 'custom';
			baseURL = s.baseURL;
			clientId = s.clientId;
			tenantId = s.tenantId;
			allowInsecureHTTP = s.allowInsecureHTTP;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		}
	}
	$effect(() => {
		load();
	});

	const custom = $derived(region === 'custom');
	const secretSet = $derived(loaded?.secretSet ?? false);

	function body(): KnoxSettingsInput | null {
		const e: Record<string, string> = {};
		if (custom && !baseURL.trim()) e.baseURL = 'Enter the base URL';
		if (!clientId.trim()) e.clientId = 'Enter the client ID';
		if (!tenantId.trim()) e.tenantId = 'Enter the tenant ID';
		if (!secretSet && !clientSecret) e.clientSecret = 'Enter the client secret';
		errors = e;
		if (Object.keys(e).length) return null;
		return {
			region,
			...(custom ? { baseURL: baseURL.trim() } : {}),
			clientId: clientId.trim(),
			tenantId: tenantId.trim(),
			...(clientSecret ? { clientSecret } : {}),
			allowInsecureHTTP: custom && allowInsecureHTTP
		};
	}

	async function test() {
		const b = body();
		if (!b) return;
		testing = true;
		result = null;
		formError = '';
		try {
			result = await testKnoxSettings(b);
		} catch (e) {
			formError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			testing = false;
		}
	}

	async function save() {
		const b = body();
		if (!b) return;
		saving = true;
		formError = '';
		try {
			const s = await saveKnoxSettings(b);
			loaded = s;
			clientSecret = '';
			knoxStatus.configured = s.configured;
			orgs.reset();
			toast.show('Knox settings saved');
			onsaved?.();
		} catch (e) {
			formError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			saving = false;
		}
	}
</script>

{#if loadError}
	<div class="py-10 text-center">
		<p class="text-[20px] font-semibold">Could not load Knox settings</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else if !loaded}
	<div class="py-10 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else}
	<form
		class="flex flex-col gap-4"
		onsubmit={(ev) => {
			ev.preventDefault();
			save();
		}}
	>
		<div>
			<label class="lbl" for="kx-region">Region</label>
			<select id="kx-region" class="field" bind:value={region}>
				{#each loaded.regions as r (r.id)}
					<option value={r.id}>{r.label}</option>
				{/each}
				<option value="custom">Custom URL</option>
			</select>
		</div>
		{#if custom}
			<Field id="kx-url" label="Base URL" bind:value={baseURL} error={errors.baseURL ?? ''} hint="For example https://eu01.manage.samsungknox.com" />
			<label class="flex min-h-11 items-center gap-3">
				<input type="checkbox" class="checkbox" bind:checked={allowInsecureHTTP} />
				<span>Allow insecure HTTP</span>
			</label>
			{#if allowInsecureHTTP}
				<Note tone="warn"><strong>Not safe.</strong> Credentials and the client secret travel unencrypted. Only use this for a test server on a trusted network.</Note>
			{/if}
		{/if}
		<Field id="kx-client" label="Client ID" bind:value={clientId} error={errors.clientId ?? ''} />
		<Field id="kx-tenant" label="Tenant ID" bind:value={tenantId} error={errors.tenantId ?? ''} />
		<Field
			id="kx-secret"
			label="Client secret"
			type="password"
			bind:value={clientSecret}
			error={errors.clientSecret ?? ''}
			hint={secretSet ? 'Set. Leave blank to keep it.' : ''}
			autocomplete="new-password"
		/>

		{#if result}
			<div class="note" class:warn={!result.ok} role={result.ok ? 'status' : 'alert'}>
				<span>
					{#if result.ok}
						<strong class="text-success">Connected.</strong> {result.message}
					{:else}
						<strong>{result.stage === 'token' ? 'Sign-in to Knox failed' : 'Knox rejected the request'}.</strong> {result.message}
					{/if}
				</span>
			</div>
		{/if}
		{#if formError}<p class="err" role="alert">{formError}</p>{/if}

		<div class="mt-2 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
			{@render children?.()}
			<button type="button" class="btn btn-soft2" disabled={testing} onclick={test}>{testing ? 'Testing...' : 'Test connection'}</button>
			<button type="submit" class="btn btn-primary" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
		</div>
	</form>
{/if}
