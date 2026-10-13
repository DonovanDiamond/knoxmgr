<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { createAppUser, deleteAppUser, listAppUsers, listAudit, updateAppUser } from '#lib/api/admin';
	import { session } from '#lib/api/session.svelte';
	import type { AppUser, AuditEntry, Role } from '#lib/api/types';
	import Avatar from '#components/Avatar.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Chip from '#components/Chip.svelte';
	import ConfirmDelete from '#components/ConfirmDelete.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Field from '#components/Field.svelte';
	import Icon from '#components/Icon.svelte';
	import Note from '#components/Note.svelte';
	import OrgSelect from '#components/OrgSelect.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';

	const isAdmin = $derived(session.user?.role === 'admin');

	let tab = $state<'accounts' | 'audit'>('accounts');
	let accounts = $state<AppUser[]>([]);
	let audit = $state<AuditEntry[]>([]);
	let loading = $state(true);
	let loadError = $state('');

	async function load() {
		loading = true;
		loadError = '';
		try {
			[accounts, audit] = await Promise.all([listAppUsers(), listAudit(), orgs.load()]).then((r) => [r[0], r[1]]);
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		if (isAdmin) load();
		else loading = false;
	});

	// Create / edit form. editing is null when creating.
	let formOpen = $state(false);
	let editing = $state<AppUser | null>(null);
	let username = $state('');
	let displayName = $state('');
	let password = $state('');
	let role = $state<Role>('user');
	let orgCode = $state('');
	let access = $state<'read' | 'write'>('read');
	let disabled = $state(false);
	let formError = $state('');
	let fieldErrors = $state<Record<string, string>>({});
	let saving = $state(false);

	function openForm(a: AppUser | null) {
		editing = a;
		username = a?.username ?? '';
		displayName = a?.displayName ?? '';
		password = '';
		role = a?.role ?? 'user';
		orgCode = a?.orgCode ?? '';
		access = a?.access ?? 'read';
		disabled = a?.disabled ?? false;
		formError = '';
		fieldErrors = {};
		formOpen = true;
	}

	function validate(): boolean {
		const e: Record<string, string> = {};
		if (!editing && !username.trim()) e.username = 'Enter a username';
		if (!editing && !password) e.password = 'Enter a password';
		if (role === 'user' && !orgCode) e.orgCode = 'Choose an organization';
		fieldErrors = e;
		return Object.keys(e).length === 0;
	}

	async function save() {
		if (!validate()) return;
		saving = true;
		formError = '';
		const scope = role === 'user' ? { orgCode, access } : {};
		try {
			if (editing) {
				// Org and access are only sent for a user-role account; the server clears them on promotion.
				await updateAppUser(editing.id, { displayName: displayName.trim(), role, disabled, ...scope });
			} else {
				await createAppUser({ username: username.trim(), displayName: displayName.trim(), password, role, ...scope });
			}
			formOpen = false;
			toast.show(editing ? 'Account saved' : 'Account created');
			await load();
		} catch (e) {
			formError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			saving = false;
		}
	}

	let deleteOpen = $state(false);
	function askDelete() {
		formOpen = false;
		deleteOpen = true;
	}
	async function remove() {
		if (!editing) return;
		await deleteAppUser(editing.id);
		toast.show('Account deleted');
		await load();
	}

	const when = (iso: string) => {
		const d = new Date(iso);
		return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
	};

	usePanel(() => ({
		head: { value: tab === 'accounts' ? accounts.length : audit.length, label: tab === 'accounts' ? 'app accounts' : 'recent changes' },
		note: 'Every change to Knox is recorded in the audit log.',
		actions: [{ label: 'Add account', kind: 'primary', onclick: () => openForm(null), disabled: !isAdmin || tab !== 'accounts', reason: isAdmin ? 'Switch to App accounts first' : 'Admins only' }]
	}));
</script>

<BigTitle title="Admin" />

{#if !isAdmin}
	<Note tone="warn" icon="lock"><strong>Admins only.</strong> Managing app accounts and viewing the audit log needs an administrator account.</Note>
{:else}
	<ul class="mb-6">
		{#each [{ href: '/admin/settings/knox', label: 'Knox connection', sub: 'Region, credentials and test' }, { href: '/admin/settings/ad', label: 'Active Directory', sub: 'Sign-in and group mappings' }, { href: '/admin/settings/general', label: 'General', sub: 'Installation name and sync interval' }, { href: '/admin/settings/apps', label: 'App categories', sub: 'Group installed apps on devices' }] as l (l.href)}
			<li class="row row-hover">
				<a href={l.href} class="rowmain">
					<span class="nm">{l.label}</span>
					<span class="sub">{l.sub}</span>
				</a>
				<span class="muted"><Icon name="chev" size={24} /></span>
			</li>
		{/each}
	</ul>

	<div class="seg mb-4" role="group" aria-label="Admin sections">
		<button type="button" aria-pressed={tab === 'accounts'} onclick={() => (tab = 'accounts')}>App accounts</button>
		<button type="button" aria-pressed={tab === 'audit'} onclick={() => (tab = 'audit')}>Audit log</button>
	</div>

	{#if loading}
		<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
	{:else if loadError}
		<div class="py-14 text-center">
			<p class="text-[20px] font-semibold">Could not load this page</p>
			<p class="muted mt-1">{loadError}</p>
			<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
		</div>
	{:else if tab === 'accounts'}
		<ul>
			{#each accounts as a (a.id)}
				<li class="row row-hover !items-start">
					<span class="mt-1"><Avatar name={a.username} small /></span>
					<button type="button" class="rowmain text-left" onclick={() => openForm(a)} aria-label="Edit {a.username}">
						<span class="nm">{a.username}{#if a.displayName}<span class="muted ml-1 text-[15px] font-normal">{a.displayName}</span>{/if}</span>
						<span class="sub !whitespace-normal">
							{a.role === 'admin' ? 'All organizations, full access' : `${orgs.name(a.orgCode)}, ${a.access} access`}
						</span>
						<span class="mt-2 flex flex-wrap gap-2">
							<Chip tone="plain">{a.authSource === 'ad' ? 'Active Directory' : 'Local account'}</Chip>
							<Chip tone="plain">{a.role === 'admin' ? 'Admin' : 'User'}</Chip>
							{#if a.disabled}<Chip tone="noemail">Disabled</Chip>{/if}
						</span>
					</button>
				</li>
			{:else}
				<li class="py-14 text-center"><p class="text-[20px] font-semibold">No app accounts</p></li>
			{/each}
		</ul>
	{:else}
		<ul>
			{#each audit as e (e.id)}
				<li class="row-hover border-b" style="border-color: var(--line)">
					<details class="py-3">
						<summary class="flex min-h-[56px] cursor-pointer items-start gap-3">
							<span class="rowmain">
								<span class="nm">{e.action}</span>
								<span class="sub !whitespace-normal">{e.target}, by {e.actorUsername || 'unknown'}</span>
								<span class="sub">{when(e.at)}</span>
							</span>
							<span class="muted pt-1"><Icon name="chev" size={20} /></span>
						</summary>
						<div class="note mt-2">
							<span class="break-words whitespace-pre-wrap">
								<strong>Previous value</strong><br />{e.previousValue ?? 'None'}
								{#if e.newValue}<br /><strong>New value</strong><br />{e.newValue}{/if}
							</span>
						</div>
					</details>
				</li>
			{:else}
				<li class="py-14 text-center"><p class="text-[20px] font-semibold">Nothing recorded yet</p></li>
			{/each}
		</ul>
	{/if}
{/if}

<Dialog bind:open={formOpen} title={editing ? `Edit ${editing.username}` : 'Add an app account'}>
	<form
		class="mt-4 flex flex-col gap-4"
		onsubmit={(ev) => {
			ev.preventDefault();
			save();
		}}
	>
		<Field id="acc-username" label="Username" bind:value={username} readonly={!!editing} error={fieldErrors.username ?? ''} autocomplete="off" />
		<Field id="acc-name" label="Display name (optional)" bind:value={displayName} />
		{#if !editing}
			<Field id="acc-password" label="Password" type="password" bind:value={password} error={fieldErrors.password ?? ''} autocomplete="new-password" />
		{/if}
		<div>
			<label class="lbl" for="acc-role">Role</label>
			<select id="acc-role" class="field" bind:value={role}>
				<option value="user">User, limited to one organization</option>
				<option value="admin">Admin, everything</option>
			</select>
		</div>
		{#if role === 'user'}
			<OrgSelect id="acc-org" label="Organization" bind:value={orgCode} placeholder="Choose an organization" error={fieldErrors.orgCode ?? ''} />
			<div>
				<label class="lbl" for="acc-access">Access</label>
				<select id="acc-access" class="field" bind:value={access}>
					<option value="read">Read, view only</option>
					<option value="write">Write, can make changes</option>
				</select>
			</div>
		{/if}
		{#if editing}
			<label class="flex min-h-11 items-center gap-3">
				<input type="checkbox" class="toggle toggle-error" bind:checked={disabled} />
				<span>Disabled, cannot sign in</span>
			</label>
		{/if}
		{#if formError}<p class="err" role="alert">{formError}</p>{/if}
		<div class="mt-2 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
			{#if editing}<button type="button" class="btn btn-quiet-danger sm:mr-auto" onclick={askDelete}>Delete account</button>{/if}
			<button type="button" class="btn btn-soft2" onclick={() => (formOpen = false)}>Cancel</button>
			<button type="submit" class="btn btn-primary" disabled={saving}>{editing ? 'Save account' : 'Create account'}</button>
		</div>
	</form>
</Dialog>

<ConfirmDelete bind:open={deleteOpen} title="Delete app account" confirmKey={editing?.username ?? ''} actionLabel="Delete account" onconfirm={remove}>
	This permanently removes the account {editing?.username}. The server refuses if it is the last enabled admin or your own account.
</ConfirmDelete>
