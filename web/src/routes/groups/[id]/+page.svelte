<script lang="ts">
	import { page } from '$app/state';
	import { ApiError } from '#lib/api/client';
	import { addMembers, getGroup, listMembers, listUsers, removeMembers } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import type { Group, KnoxUser } from '#lib/api/types';
	import Avatar from '#components/Avatar.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Note from '#components/Note.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { accessOf, displayName } from '#lib/perms';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';
	import { SvelteSet } from 'svelte/reactivity';

	const id = $derived(page.params.id ?? '');

	let group = $state<Group | null>(null);
	let members = $state<KnoxUser[]>([]);
	let loading = $state(true);
	let loadError = $state('');
	let actionError = $state('');

	let pickerOpen = $state(false);
	let candidates = $state<KnoxUser[]>([]);
	let pickerLoading = $state(false);
	let pickerError = $state('');
	let pq = $state('');
	let adding = $state(false);
	const picked = new SvelteSet<string>();

	const access = $derived(accessOf(session.user, orgs.name(session.user?.orgCode)));
	const scopeName = $derived(orgs.name(access.scopeOrg));
	const title = $derived(group?.groupName ?? '');

	const pickList = $derived.by(() => {
		const have = new Set(members.map((m) => m.userId));
		const inScope = access.scopeOrg ? orgs.scope(access.scopeOrg) : null;
		const needle = pq.trim().toLowerCase();
		return candidates.filter(
			(u) =>
				!have.has(u.userId) &&
				(!inScope || inScope.has(u.orgCode)) &&
				(!needle || `${displayName(u)} ${u.userId}`.toLowerCase().includes(needle))
		);
	});

	async function load() {
		loading = true;
		loadError = '';
		try {
			const [g, m] = await Promise.all([getGroup(id), listMembers(id), orgs.load()]);
			group = g;
			members = m;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	async function remove(u: KnoxUser) {
		actionError = '';
		try {
			await removeMembers(id, [u.userId]);
			members = members.filter((m) => m.userId !== u.userId);
			toast.show(`Removed from ${title}`);
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		}
	}

	async function openPicker() {
		pq = '';
		picked.clear();
		pickerError = '';
		pickerOpen = true;
		pickerLoading = true;
		try {
			candidates = await listUsers();
		} catch (e) {
			pickerError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			pickerLoading = false;
		}
	}

	function togglePick(userId: string, on: boolean) {
		if (on) picked.add(userId);
		else picked.delete(userId);
	}

	async function add() {
		const ids = [...picked];
		adding = true;
		pickerError = '';
		try {
			await addMembers(id, ids);
			pickerOpen = false;
			toast.show(`Added ${ids.length} to ${title}`);
			members = await listMembers(id);
		} catch (e) {
			pickerError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			adding = false;
		}
	}

	usePanel(() => ({
		head: { value: members.length, label: members.length === 1 ? 'member shown' : 'members shown' },
		note: access.canWrite ? '' : access.reason,
		lock: !access.canWrite,
		actions: [{ label: 'Add members', kind: 'primary', onclick: openPicker, disabled: !access.canWrite || !group, reason: access.reason }]
	}));
</script>

<BigTitle {title} back="/groups" />

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load this group</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	{#if access.scopeOrg && scopeName}
		<div class="mb-4"><Note>Showing members in {scopeName} and its sub-organizations.</Note></div>
	{/if}
	{#if !access.canWrite}
		<div class="mb-4"><Note tone="warn" icon="lock">{access.reason}.</Note></div>
	{/if}
	{#if actionError}<div class="note warn my-3" role="alert"><span>{actionError}</span></div>{/if}

	<p class="muted py-2 text-[15px]" aria-live="polite">{members.length} {members.length === 1 ? 'member' : 'members'} shown</p>
	<ul>
		{#each members as u (u.userId)}
			<li class="row row-hover">
				<Avatar name={displayName(u)} seed={u.userId} small />
				<span class="rowmain">
					<span class="nm">{displayName(u)}</span>
					<span class="sub">{u.orgName || orgs.name(u.orgCode)}</span>
				</span>
				<DeviceBadge count={u.devices.enrolled} freshness={u.devices.freshness} />
				<button
					type="button"
					class="btn btn-quiet-danger !min-h-11 !px-1 sm:!px-3"
					disabled={!access.canWrite}
					title={access.canWrite ? undefined : access.reason}
					aria-label="Remove {displayName(u)} from {title}"
					onclick={() => remove(u)}>Remove</button>
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">No members yet</p>
				<p class="muted mt-1">Add people so they get this group in Knox.</p>
			</li>
		{/each}
	</ul>
{/if}

<Dialog bind:open={pickerOpen} title="Add members">
	<p class="muted mt-1">to {title}{scopeName ? `. You can add people in ${scopeName} and its sub-organizations.` : ''}</p>
	<label class="sr-only" for="pq">Search users</label>
	<input id="pq" type="search" class="field mt-4" placeholder="Search name or user ID" autocomplete="off" bind:value={pq} />
	{#if pickerLoading}
		<div class="py-8 text-center"><span class="loading loading-spinner"></span></div>
	{:else}
		<ul class="mt-2 max-h-[40vh] overflow-y-auto px-3">
			{#each pickList as u (u.userId)}
				<li>
					<label class="row row-hover !min-h-[64px]">
						<input type="checkbox" class="checkbox checkbox-primary" checked={picked.has(u.userId)} onchange={(e) => togglePick(u.userId, e.currentTarget.checked)} />
						<Avatar name={displayName(u)} seed={u.userId} small />
						<span class="rowmain">
							<span class="nm !text-[16px]">{displayName(u)}</span>
							<span class="sub">{u.orgName || orgs.name(u.orgCode)}</span>
						</span>
					</label>
				</li>
			{:else}
				<li class="muted py-8 text-center">Nobody left to add. Try a different search.</li>
			{/each}
		</ul>
	{/if}
	{#if pickerError}<p class="err" role="alert">{pickerError}</p>{/if}
	<div class="mt-5 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => (pickerOpen = false)}>Cancel</button>
		<button type="button" class="btn btn-primary" disabled={!picked.size || adding} onclick={add}>
			{picked.size ? `Add ${picked.size} ${picked.size === 1 ? 'member' : 'members'}` : 'Add members'}
		</button>
	</div>
</Dialog>
