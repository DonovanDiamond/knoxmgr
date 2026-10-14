<script lang="ts">
	import { goto } from '$app/navigation';
	import { ApiError } from '#lib/api/client';
	import { inviteUsers, listUsers } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import type { EnrollmentResult, KnoxUser } from '#lib/api/types';
	import Avatar from '#components/Avatar.svelte';
	import KnoxOffline from '#components/KnoxOffline.svelte';
	import { knoxStatus } from '#lib/health.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';
	import Icon from '#components/Icon.svelte';
	import InviteResultDialog from '#components/InviteResultDialog.svelte';
	import Note from '#components/Note.svelte';
	import OrgSelect from '#components/OrgSelect.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { accessOf, displayName } from '#lib/perms';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';
	import { SvelteSet } from 'svelte/reactivity';

	let users = $state<KnoxUser[]>([]);
	let loading = $state(true);
	let loadError = $state('');
	let q = $state('');
	let orgFilter = $state('');
	const selected = new SvelteSet<string>();
	let inviting = $state(false);
	let inviteOpen = $state(false);
	let inviteUsersSent = $state<KnoxUser[]>([]);
	let inviteResult = $state<EnrollmentResult | null>(null);
	let actionError = $state('');

	const access = $derived(accessOf(session.user, orgs.name(session.user?.orgCode)));
	const scopeName = $derived(orgs.name(access.scopeOrg));

	async function load() {
		loading = true;
		loadError = '';
		try {
			await Promise.all([listUsers().then((u) => (users = u)), orgs.load()]);
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	const visible = $derived.by(() => {
		const inOrg = orgFilter ? orgs.scope(orgFilter) : null;
		const needle = q.trim().toLowerCase();
		return users.filter(
			(u) =>
				(!inOrg || inOrg.has(u.orgCode)) &&
				(!needle || `${displayName(u)} ${u.userId} ${u.email}`.toLowerCase().includes(needle))
		);
	});

	function toggle(id: string) {
		if (selected.has(id)) selected.delete(id);
		else selected.add(id);
	}

	async function invite() {
		const ids = [...selected];
		inviting = true;
		actionError = '';
		try {
			inviteResult = await inviteUsers(ids);
			inviteUsersSent = users.filter((u) => selected.has(u.userId));
			inviteOpen = true;
			selected.clear();
			listUsers().then((u) => (users = u), () => {});
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not reach the server.';
			toast.show('Invitations were not sent');
		} finally {
			inviting = false;
		}
	}

	usePanel(() => ({
		head: { value: selected.size, label: 'selected' },
		note: access.canWrite
			? selected.size ? '' : 'Tap an avatar to select people for an invite.'
			: access.reason,
		lock: !access.canWrite,
		actions: [
			{
				label: selected.size ? `Send enrollment invite (${selected.size})` : 'Send enrollment invite',
				kind: selected.size ? 'primary' : 'soft',
				onclick: invite,
				disabled: !access.canWrite || !selected.size || inviting,
				reason: access.canWrite ? 'Select at least one person first' : access.reason
			},
			...(selected.size ? [] : [{ label: 'Add user', kind: 'primary' as const, lead: true, onclick: () => goto('/users/new'), disabled: !access.canWrite, reason: access.reason }])
		]
	}));
</script>

<BigTitle title="Users" />

{#if access.scopeOrg && scopeName}
	<div class="mb-5">
		{#if access.canWrite}
			<Note>Showing people in {scopeName} and its sub-organizations.</Note>
		{:else}
			<Note tone="warn" icon="lock"><strong>{access.reason}.</strong> You can view people in {scopeName} and its sub-organizations but not change them.</Note>
		{/if}
	</div>
{:else if !access.canWrite && session.user}
	<div class="mb-5"><Note tone="warn" icon="lock">{access.reason}</Note></div>
{/if}

<div class="mb-2 flex flex-col gap-3 sm:flex-row">
	<div class="relative flex-1">
		<span class="muted absolute top-1/2 left-3 -translate-y-1/2"><Icon name="search" size={20} /></span>
		<label class="sr-only" for="q">Search users</label>
		<input id="q" type="search" class="field !pl-10" placeholder="Search name, email or user ID" autocomplete="off" bind:value={q} />
	</div>
	<div class="sm:w-56">
		<OrgSelect id="orgf" label="Organization" bind:value={orgFilter} root={access.scopeOrg} allLabel={scopeName ? `All of ${scopeName}` : 'All organizations'} />
	</div>
</div>

{#if actionError}<div class="note warn my-3" role="alert"><span>{actionError}</span></div>{/if}

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if knoxStatus.configured === false}
	<KnoxOffline />
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load users</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<p class="muted py-2 text-[15px]" aria-live="polite">
		{visible.length} {visible.length === 1 ? 'person' : 'people'}{q || orgFilter ? ' match' : ''}
	</p>
	<ul>
		{#each visible as u (u.userId)}
			<li class="row row-hover">
				<Avatar name={displayName(u)} seed={u.userId} selected={selected.has(u.userId)} disabled={!access.canWrite} onclick={() => toggle(u.userId)} />
				<a href="/users/{encodeURIComponent(u.userId)}" class="rowmain">
					<span class="nm">{displayName(u)} <span class="muted ml-1 hidden text-[15px] font-normal sm:inline">{u.userId}</span></span>
					<span class="sub">{u.email || 'No email address on file'}</span>
					<span class="sub">{u.orgName || orgs.name(u.orgCode)}</span>
				</a>
				<DeviceBadge count={u.devices.enrolled} freshness={u.devices.freshness} />
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">{users.length ? 'Nobody matches that search' : 'No users yet'}</p>
				<p class="muted mt-1">{users.length ? 'Check the spelling, or clear the organization filter.' : 'Users you add will appear here.'}</p>
				{#if access.canWrite}<a href="/users/new" class="btn btn-primary mt-5">Add a new user</a>{/if}
			</li>
		{/each}
	</ul>
{/if}

<InviteResultDialog bind:open={inviteOpen} users={inviteUsersSent} result={inviteResult} />
