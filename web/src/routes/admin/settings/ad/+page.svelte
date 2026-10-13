<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { getAdSettings, saveAdSettings, testAdSignIn } from '#lib/api/settings';
	import { session } from '#lib/api/session.svelte';
	import type { AdMapping, AdSettings, AdSettingsInput, AdTestResult } from '#lib/api/types';
	import BigTitle from '#components/BigTitle.svelte';
	import Field from '#components/Field.svelte';
	import Note from '#components/Note.svelte';
	import OrgSelect from '#components/OrgSelect.svelte';
	import { knoxStatus } from '#lib/health.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { toast } from '#lib/toast.svelte';

	const isAdmin = $derived(session.user?.role === 'admin');

	let loaded = $state<AdSettings | null>(null);
	let loadError = $state('');
	let enabled = $state(false);
	let url = $state('');
	let baseDN = $state('');
	let upnSuffix = $state('');
	let bindUser = $state('');
	let bindPassword = $state('');
	let mappings = $state<AdMapping[]>([]);

	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let saving = $state(false);

	async function load() {
		loadError = '';
		try {
			const [s] = await Promise.all([getAdSettings(), orgs.load()]);
			loaded = s;
			enabled = s.enabled;
			url = s.url;
			baseDN = s.baseDN;
			upnSuffix = s.upnSuffix;
			bindUser = s.bindUser;
			mappings = s.mappings.map((m) => ({ ...m }));
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		}
	}
	$effect(() => {
		if (isAdmin) load();
	});

	function addRow() {
		mappings.push({ group: '', role: 'user', orgCode: '', access: 'read' });
	}
	function removeRow(i: number) {
		mappings.splice(i, 1);
	}
	// Order matters when a person is in several mapped groups, so rows can be reordered.
	function move(i: number, d: -1 | 1) {
		const j = i + d;
		if (j < 0 || j >= mappings.length) return;
		[mappings[i], mappings[j]] = [mappings[j], mappings[i]];
	}

	function body(): AdSettingsInput {
		return {
			enabled,
			url: url.trim(),
			baseDN: baseDN.trim(),
			upnSuffix: upnSuffix.trim(),
			bindUser: bindUser.trim(),
			...(bindPassword ? { bindPassword } : {}),
			mappings: mappings.map((m) => ({
				group: m.group.trim(),
				role: m.role,
				orgCode: m.role === 'user' ? m.orgCode : '',
				// The backend rejects an access value on admin mappings. The type has no "" member, hence the cast.
				access: m.role === 'user' ? m.access : ('' as AdMapping['access'])
			}))
		};
	}

	function validate(): boolean {
		const e: Record<string, string> = {};
		if (enabled) {
			if (!url.trim()) e.url = 'Enter the server URL';
			if (!baseDN.trim()) e.baseDN = 'Enter the base DN';
			if (!upnSuffix.trim()) e.upnSuffix = 'Enter the UPN suffix';
		}
		mappings.forEach((m, i) => {
			if (!m.group.trim()) e[`g${i}`] = 'Enter a group';
			if (m.role === 'user' && !m.orgCode.trim()) e[`o${i}`] = 'Choose an organization';
		});
		errors = e;
		return Object.keys(e).length === 0;
	}

	async function save(ev: SubmitEvent) {
		ev.preventDefault();
		if (!validate()) return;
		saving = true;
		formError = '';
		try {
			const s = await saveAdSettings(body());
			loaded = s;
			bindPassword = '';
			toast.show('Active Directory settings saved');
		} catch (e) {
			formError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			saving = false;
		}
	}

	let tUser = $state('');
	let tPass = $state('');
	let testing = $state(false);
	let tResult = $state<AdTestResult | null>(null);
	let tError = $state('');

	async function runTest(ev: SubmitEvent) {
		ev.preventDefault();
		testing = true;
		tResult = null;
		tError = '';
		try {
			tResult = await testAdSignIn(tUser.trim(), tPass, body());
		} catch (e) {
			tError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			testing = false;
		}
	}
</script>

<BigTitle title="Active Directory" back="/admin" />

{#if !isAdmin}
	<Note tone="warn" icon="lock"><strong>Admins only.</strong> Settings need an administrator account.</Note>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load settings</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else if !loaded}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else}
	<form class="flex max-w-3xl flex-col gap-4" onsubmit={save}>
		<label class="flex min-h-11 items-center gap-3">
			<input type="checkbox" class="toggle toggle-primary" bind:checked={enabled} />
			<span>Allow sign-in with Active Directory</span>
		</label>
		<Field id="ad-url" label="Server URL" bind:value={url} error={errors.url ?? ''} hint="For example ldaps://dc.example.com:636" />
		<Field id="ad-dn" label="Base DN" bind:value={baseDN} error={errors.baseDN ?? ''} hint="For example DC=example,DC=com" />
		<Field id="ad-upn" label="UPN suffix" bind:value={upnSuffix} error={errors.upnSuffix ?? ''} hint="For example example.com, so jsmith signs in as jsmith@example.com" />
		<!-- Bind user/password are not used (go-ad-auth binds as the signing-in user); kept in the payload only. -->

		<h2 class="mt-4 text-[20px] font-semibold">Group mappings</h2>
		<p class="muted">
			Each AD group gives its members a role. Rows are checked from the top, and the first match wins.
			{#if !knoxStatus.configured}Knox is not connected, so type organization codes by hand.{/if}
		</p>
		{#each mappings as m, i (i)}
			<div class="rounded-2xl border p-3" style="border-color: var(--line)">
				<div class="flex flex-col gap-3 sm:flex-row">
					<div class="sm:flex-1"><Field id="m-g{i}" label="AD group" bind:value={m.group} error={errors[`g${i}`] ?? ''} /></div>
					<div class="sm:w-40">
						<label class="lbl" for="m-r{i}">Role</label>
						<select id="m-r{i}" class="field" bind:value={m.role}>
							<option value="user">User</option>
							<option value="admin">Admin</option>
						</select>
					</div>
				</div>
				{#if m.role === 'user'}
					<div class="mt-3 flex flex-col gap-3 sm:flex-row">
						<div class="sm:flex-1">
							{#if knoxStatus.configured}
								<OrgSelect id="m-o{i}" label="Organization" bind:value={m.orgCode} placeholder="Choose an organization" error={errors[`o${i}`] ?? ''} />
							{:else}
								<Field id="m-o{i}" label="Organization code" bind:value={m.orgCode} error={errors[`o${i}`] ?? ''} />
							{/if}
						</div>
						<div class="sm:w-40">
							<label class="lbl" for="m-a{i}">Access</label>
							<select id="m-a{i}" class="field" bind:value={m.access}>
								<option value="read">Read</option>
								<option value="write">Write</option>
							</select>
						</div>
					</div>
				{:else}
					<p class="hint">Admins see every organization with full access.</p>
				{/if}
				<div class="mt-3 flex gap-2">
					<button type="button" class="btn btn-soft2 !min-h-11 !px-3" disabled={i === 0} aria-label="Move group {i + 1} up" onclick={() => move(i, -1)}>Up</button>
					<button type="button" class="btn btn-soft2 !min-h-11 !px-3" disabled={i === mappings.length - 1} aria-label="Move group {i + 1} down" onclick={() => move(i, 1)}>Down</button>
					<button type="button" class="btn btn-quiet-danger !min-h-11 !px-3 ml-auto" aria-label="Remove group {i + 1}" onclick={() => removeRow(i)}>Remove</button>
				</div>
			</div>
		{:else}
			<Note>No mappings yet. Nobody can sign in with Active Directory until a group is mapped.</Note>
		{/each}
		<div><button type="button" class="btn btn-soft2" onclick={addRow}>Add mapping</button></div>

		{#if formError}<Note tone="warn"><strong>Could not save.</strong> {formError}</Note>{/if}
		<div class="mt-2 flex sm:justify-end">
			<button type="submit" class="btn btn-primary w-full sm:w-auto" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
		</div>
	</form>

	<form class="mt-10 flex max-w-xl flex-col gap-4" onsubmit={runTest}>
		<h2 class="text-[20px] font-semibold">Test sign-in</h2>
		<p class="muted">Tries the settings above, even if unsaved. Nothing is changed.</p>
		<Field id="t-u" label="Username" bind:value={tUser} autocomplete="off" />
		<Field id="t-p" label="Password" type="password" bind:value={tPass} autocomplete="off" />
		<div><button class="btn btn-soft2" disabled={testing || !tUser.trim() || !tPass}>{testing ? 'Testing...' : 'Test sign-in'}</button></div>
		{#if tError}<Note tone="warn"><strong>Could not run the test.</strong> {tError}</Note>{/if}
		{#if tResult}
			{#if tResult.ok}
				<div class="note" role="status">
					<span>
						<strong class="text-success">Signed in as {tResult.displayName}.</strong><br />
						Role: {tResult.role === 'admin' ? 'Admin' : 'User'}{#if tResult.role === 'user'}, organization {orgs.name(tResult.orgCode)}, {tResult.access} access{/if}<br />
						Groups: {tResult.groups.join(', ') || 'none'}
					</span>
				</div>
			{:else if tResult.code === 'ad_unreachable'}
				<Note tone="warn"><strong>Directory unreachable.</strong> {tResult.message} This is a connection problem, not a wrong password. Check the server URL and network.</Note>
			{:else}
				<div class="note" role="alert"><span><strong>{tResult.code}.</strong> {tResult.message}</span></div>
			{/if}
		{/if}
	</form>
{/if}
