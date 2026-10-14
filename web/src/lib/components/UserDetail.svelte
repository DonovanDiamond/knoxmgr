<script lang="ts">
	import { goto } from '$app/navigation';
	import { listUserDevices, type Device } from '#lib/api/devices';
	import { ApiError } from '#lib/api/client';
	import { addMembers, getUser, getUserGroups, inviteUsers, listGroups, purgeUser, removeMembers } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import type { EnrollmentResult, Group, KnoxUser } from '#lib/api/types';
	import { orgs } from '#lib/orgs.svelte';
	import { accessOf, displayName } from '#lib/perms';
	import { toast } from '#lib/toast.svelte';
	import Avatar from './Avatar.svelte';
	import BigTitle from './BigTitle.svelte';
	import ConfirmDelete from './ConfirmDelete.svelte';
	import DeviceBadge from './DeviceBadge.svelte';
	import Dialog from './Dialog.svelte';
	import InviteResultDialog from './InviteResultDialog.svelte';
	import Note from './Note.svelte';
	import SearchPicker from './SearchPicker.svelte';

	// Read view of one Knox user: info, group memberships and devices. onedit switches the page to the editor.
	let { id, onedit }: { id: string; onedit: () => void } = $props();

	let user = $state<KnoxUser | null>(null);
	let groups = $state<Group[]>([]);
	let devices = $state<Device[]>([]);

	// Enrolled devices first, then most recently connected; devices that never connected go last.
	function sortDevices(list: Device[]): Device[] {
		const t = (d: Device) => (d.lastConnection ? Date.parse(d.lastConnection) || 0 : 0);
		return [...list].sort((x, y) => Number(y.enrolled) - Number(x.enrolled) || t(y) - t(x));
	}
	let loading = $state(true);
	let loadError = $state('');
	let actionError = $state('');
	// Groups and devices load separately so a failure there does not hide the user.
	let sideError = $state('');

	let confirmOpen = $state(false);
	let inviting = $state(false);
	let inviteOpen = $state(false);
	let inviteResult = $state<EnrollmentResult | null>(null);

	let pickerOpen = $state(false);
	let allGroups = $state<Group[]>([]);
	let pickerLoading = $state(false);
	let pickerError = $state('');
	let adding = $state('');
	let groupFilter = $state('');

	const access = $derived(accessOf(session.user, orgs.name(session.user?.orgCode)));
	const noEmail = $derived(!!user && !user.email.trim());
	const inviteReason = $derived(access.canWrite ? 'This person has no email address. Edit them to add one.' : access.reason);
	const pickList = $derived(allGroups.filter((g) => !groups.some((m) => m.groupId === g.groupId)));
	const shownGroups = $derived(groups.filter((g) => g.groupName.toLowerCase().includes(groupFilter.trim().toLowerCase())));
	const fmt = (iso?: string) => (iso ? new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' }) : '');

	async function load() {
		loading = true;
		loadError = '';
		try {
			user = await getUser(id);
			await orgs.load().catch(() => {});
		} catch (e) {
			loadError =
				e instanceof ApiError && e.status === 404
					? `This user does not exist, or is outside your organization${access.scopeOrg ? ` (${orgs.name(access.scopeOrg)} and its sub-organizations)` : ''}.`
					: e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
		if (loadError) return;
		sideError = '';
		const [g, d] = await Promise.allSettled([getUserGroups(id), listUserDevices(id)]);
		if (g.status === 'fulfilled') groups = g.value;
		if (d.status === 'fulfilled') devices = sortDevices(d.value.devices);
		const failed = [g, d].find((r) => r.status === 'rejected');
		if (failed?.status === 'rejected')
			sideError = failed.reason instanceof ApiError ? failed.reason.message : 'Could not reach the server.';
	}
	$effect(() => {
		load();
	});

	async function invite() {
		if (!user) return;
		inviting = true;
		actionError = '';
		try {
			inviteResult = await inviteUsers([id]);
			inviteOpen = true;
			user = await getUser(id);
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not reach the server.';
			toast.show('Invitation was not sent');
		} finally {
			inviting = false;
		}
	}

	async function purge() {
		await purgeUser(id, id);
		toast.show(`Deleted ${user ? displayName(user) : id}`);
		await goto('/users');
	}

	async function removeFrom(g: Group) {
		actionError = '';
		try {
			await removeMembers(g.groupId, [id]);
			groups = groups.filter((x) => x.groupId !== g.groupId);
			toast.show(`Removed from ${g.groupName}`);
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		}
	}

	async function openPicker() {
		pickerError = '';
		pickerOpen = true;
		pickerLoading = true;
		try {
			allGroups = await listGroups();
		} catch (e) {
			pickerError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			pickerLoading = false;
		}
	}

	async function addTo(g: Group) {
		adding = g.groupId;
		pickerError = '';
		try {
			await addMembers(g.groupId, [id]);
			groups = [...groups, g];
			toast.show(`Added to ${g.groupName}`);
			if (!pickList.length) pickerOpen = false;
		} catch (e) {
			pickerError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			adding = '';
		}
	}
</script>

<BigTitle title={user ? displayName(user) : 'User'} back="/users">
	{#snippet actions()}
		<div class="flex flex-wrap items-center justify-end gap-2 rounded-2xl bg-primary/10 p-2">
			<button type="button" class="btn btn-primary !min-h-11" disabled={!user || !access.canWrite} title={access.canWrite ? undefined : access.reason} onclick={onedit}>Edit</button>
			<button type="button" class="btn btn-soft2 !min-h-11" disabled={!user || !access.canWrite || noEmail || inviting} title={!user || !access.canWrite || noEmail ? inviteReason : undefined} onclick={invite}>Send enrollment invite</button>
			<button type="button" class="btn btn-quiet-danger !min-h-11" disabled={!user || !access.canWrite} title={access.canWrite ? undefined : access.reason} onclick={() => (confirmOpen = true)}>Delete user</button>
		</div>
	{/snippet}
</BigTitle>

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<Note tone="warn" icon="lock">{loadError}</Note>
	<a href="/users" class="btn btn-soft2 mt-5">Back to users</a>
{:else if user}
	{#if !access.canWrite}
		<div class="mb-5"><Note tone="warn" icon="lock"><strong>{access.reason}.</strong> You can view this person but not change them.</Note></div>
	{/if}
	{#if actionError}<div class="note warn my-3" role="alert"><span>{actionError}</span></div>{/if}

	<section class="mt-4 flex items-start gap-4" aria-label="User details">
		<Avatar name={displayName(user)} seed={user.userId} />
		<dl class="grid min-w-0 flex-1 gap-x-8 gap-y-2 sm:grid-cols-2">
			{#each [
				['User ID', user.userId],
				['Email', user.email],
				['Organization', user.orgName || orgs.name(user.orgCode)],
				['Contact number', user.contact || user.mobilePhone],
				['Employee number', user.empNo],
				['Last invited', fmt(user.invitedAt)]
			] as [label, value] (label)}
				<div class="min-w-0">
					<dt class="muted text-[14px]">{label}</dt>
					<dd class="break-words">{value || (label === 'Email' ? 'No email address on file' : label === 'Last invited' ? 'Never' : 'Not set')}</dd>
				</div>
			{/each}
		</dl>
	</section>

	{#if sideError}<div class="note warn my-3" role="alert"><span>{sideError}</span></div>{/if}

	<div class="mt-8 grid gap-8 lg:grid-cols-2">
		<section aria-labelledby="u-groups">
			<div class="flex items-center justify-between gap-3">
				<h2 id="u-groups" class="text-[20px] font-semibold">Groups</h2>
				<button type="button" class="btn btn-soft2 !min-h-11" disabled={!access.canWrite} title={access.canWrite ? undefined : access.reason} onclick={openPicker}>Add to group</button>
			</div>
			{#if groups.length > 8}
				<input type="search" class="input input-bordered mt-2 w-full" placeholder="Filter groups" aria-label="Filter groups" bind:value={groupFilter} />
			{/if}
			<ul>
				{#each shownGroups as g (g.groupId)}
					<li class="row row-hover">
						<a href="/groups/{encodeURIComponent(g.groupId)}" class="rowmain"><span class="nm">{g.groupName}</span></a>
						<button
							type="button"
							class="btn btn-quiet-danger !min-h-11 !px-1 sm:!px-3"
							disabled={!access.canWrite}
							title={access.canWrite ? undefined : access.reason}
							aria-label="Remove from {g.groupName}"
							onclick={() => removeFrom(g)}>Remove</button>
					</li>
				{:else}
					<li class="muted py-8 text-center">{groups.length ? 'No matching groups.' : 'Not in any group you can see.'}</li>
				{/each}
			</ul>
		</section>

		<section aria-labelledby="u-devices">
			<h2 id="u-devices" class="text-[20px] font-semibold">Devices</h2>
			<ul>
				{#each devices as d (d.deviceId)}
					<li>
						<a href="/devices/{encodeURIComponent(d.deviceId)}" class="row row-hover">
							<span class="rowmain">
								<span class="nm">{d.modelName || d.deviceName || d.deviceId}</span>
								<span class="sub">{d.os}{d.osVersion ? ` ${d.osVersion}` : ''}</span>
								<span class="sub">{d.lastConnection ? `Last connected ${fmt(d.lastConnection)}` : d.freshness === 'not_enrolled' ? 'Not enrolled' : 'Never connected'}</span>
							</span>
							<DeviceBadge freshness={d.freshness} />
						</a>
					</li>
				{:else}
					<li class="muted py-8 text-center">No devices.</li>
				{/each}
			</ul>
		</section>
	</div>
{/if}

<ConfirmDelete bind:open={confirmOpen} title="Delete user?" confirmKey={id} actionLabel="Delete user" onconfirm={purge}>
	This permanently deletes <strong>{user ? displayName(user) : id}</strong> from Knox. It cannot be undone, and their phone will lose its enrollment.
</ConfirmDelete>

<InviteResultDialog bind:open={inviteOpen} users={user ? [user] : []} result={inviteResult} />

<Dialog bind:open={pickerOpen} title="Add to group">
	{#if pickerLoading}
		<div class="py-8 text-center"><span class="loading loading-spinner"></span></div>
	{:else}
		{#if pickList.length}
			<div class="mt-3"><SearchPicker items={pickList} label={(g) => g.groupName} onpick={addTo} placeholder="Search groups" disabled={!!adding} /></div>
		{:else}
			<p class="muted py-8 text-center">Already in every group.</p>
		{/if}
	{/if}
	{#if pickerError}<p class="err" role="alert">{pickerError}</p>{/if}
	<button type="button" class="btn btn-soft2 mt-5 w-full" onclick={() => (pickerOpen = false)}>Done</button>
</Dialog>
