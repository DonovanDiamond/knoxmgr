<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '#lib/api/client';
	import { deleteOrg, getOrg, listUsers, moveUsersToOrg, updateOrg } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import type { KnoxUser, OrgDetail } from '#lib/api/types';
	import Avatar from '#components/Avatar.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import DeviceBadge from '#components/DeviceBadge.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Field from '#components/Field.svelte';
	import Icon from '#components/Icon.svelte';
	import OrgSelect from '#components/OrgSelect.svelte';
	import SearchPicker from '#components/SearchPicker.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { displayName } from '#lib/perms';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';

	const code = $derived(page.params.code ?? '');

	let detail = $state<OrgDetail | null>(null);
	let loading = $state(true);
	let loadError = $state('');
	let q = $state('');

	const isAdmin = $derived(session.user?.role === 'admin');
	const title = $derived(detail?.org.orgName ?? '');
	// A top-level org has no parent to pick and cannot be given one.
	const topLevel = $derived(!!detail && !detail.org.parentCode);
	const subOrgs = $derived(orgs.list.filter((o) => o.parentCode === code));
	const members = $derived(detail?.members ?? []);
	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		return members.filter((m) => !needle || `${displayName(m)} ${m.userId} ${m.email}`.toLowerCase().includes(needle));
	});

	let editOpen = $state(false);
	let editName = $state('');
	let editParent = $state('');
	let nameError = $state('');
	let editError = $state('');
	let saving = $state(false);

	let deleteOpen = $state(false);
	let deleteError = $state('');
	let deleting = $state(false);

	let moveOpen = $state(false);
	let candidates = $state<KnoxUser[]>([]);
	let chosen = $state<KnoxUser[]>([]);
	let moveLoading = $state(false);
	let moveError = $state('');
	let moveConfirm = $state(false);
	let moving = $state(false);
	const pickable = $derived(candidates.filter((u) => u.orgCode !== code && !chosen.some((c) => c.userId === u.userId)));

	async function load() {
		loading = true;
		loadError = '';
		try {
			const [d] = await Promise.all([getOrg(code), orgs.load()]);
			detail = d;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	function openEdit() {
		if (!detail) return;
		editName = detail.org.orgName;
		editParent = detail.org.parentCode;
		nameError = editError = '';
		editOpen = true;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		const n = editName.trim();
		nameError = n ? '' : 'Enter an organization name.';
		editError = '';
		if (nameError) return;
		saving = true;
		try {
			await updateOrg(code, { orgName: n, parentCode: editParent });
			editOpen = false;
			toast.show('Organization saved');
			await Promise.all([orgs.refresh(), load()]);
		} catch (err) {
			editError = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			saving = false;
		}
	}

	async function remove() {
		deleting = true;
		deleteError = '';
		try {
			await deleteOrg(code);
			deleteOpen = false;
			toast.show(`Deleted ${title}`);
			await orgs.refresh();
			goto('/orgs');
		} catch (err) {
			// Knox's refusal (e.g. the org still has users) is shown verbatim.
			deleteError = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			deleting = false;
		}
	}

	async function openMove() {
		chosen = [];
		moveError = '';
		moveOpen = true;
		moveLoading = true;
		try {
			candidates = await listUsers();
		} catch (e) {
			moveError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			moveLoading = false;
		}
	}

	async function move() {
		moving = true;
		moveError = '';
		try {
			await moveUsersToOrg(code, chosen.map((u) => u.userId));
			moveConfirm = false;
			moveOpen = false;
			toast.show(`Moved ${chosen.length} to ${title}`);
			await load();
		} catch (e) {
			moveConfirm = false;
			moveOpen = true;
			moveError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			moving = false;
		}
	}

	usePanel(() => ({
		head: { value: visible.length, label: visible.length === 1 ? 'member shown' : 'members shown' },
		note: isAdmin ? '' : 'Only admins edit organizations.',
		lock: !isAdmin,
		actions: isAdmin
			? [
					{ label: 'Move users here', kind: 'primary', onclick: openMove, disabled: !detail },
					{ label: 'Edit', kind: 'soft', onclick: openEdit, disabled: !detail },
					{ label: 'Delete organization', kind: 'outline-danger', onclick: () => ((deleteError = ''), (deleteOpen = true)), disabled: !detail }
				]
			: []
	}));
</script>

<BigTitle {title} back="/orgs" />

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load this organization</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else if detail}
	<dl class="mb-4 grid gap-x-8 gap-y-3 sm:grid-cols-3">
		<div><dt class="muted text-[15px]">Name</dt><dd class="text-[18px]">{detail.org.orgName}</dd></div>
		<div><dt class="muted text-[15px]">Code</dt><dd class="text-[18px] break-all">{detail.org.orgCode}</dd></div>
		<div>
			<dt class="muted text-[15px]">Parent</dt>
			<dd class="text-[18px]">
				{#if detail.org.parentCode}
					<a href="/orgs/{encodeURIComponent(detail.org.parentCode)}" class="link">{orgs.name(detail.org.parentCode)}</a>
				{:else}None{/if}
			</dd>
		</div>
	</dl>

	{#if subOrgs.length}
		<h2 class="muted mb-1 text-[15px]">Sub-organizations</h2>
		<ul class="mb-4 flex flex-wrap gap-x-4 gap-y-1">
			{#each subOrgs as o (o.orgCode)}
				<li><a href="/orgs/{encodeURIComponent(o.orgCode)}" class="link text-[18px]">{o.orgName}</a></li>
			{/each}
		</ul>
	{/if}

	<h2 class="muted mb-1 text-[15px]">Members</h2>
	<div class="relative mb-2">
		<span class="muted absolute top-1/2 left-3 -translate-y-1/2"><Icon name="search" size={20} /></span>
		<label class="sr-only" for="mq">Search members</label>
		<input id="mq" type="search" class="field !pl-10" placeholder="Search name, email or user ID" autocomplete="off" bind:value={q} />
	</div>
	<ul>
		{#each visible as u (u.userId)}
			<li class="row row-hover">
				<Avatar name={displayName(u)} seed={u.userId} />
				<a href="/users/{encodeURIComponent(u.userId)}" class="rowmain">
					<span class="nm">{displayName(u)}</span>
					<span class="sub">{u.userId}</span>
				</a>
				<DeviceBadge count={u.devices.enrolled} freshness={u.devices.freshness} />
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">{members.length ? 'Nobody matches that search' : 'No members'}</p>
			</li>
		{/each}
	</ul>
{/if}

<Dialog bind:open={editOpen} title="Edit organization">
	<form novalidate onsubmit={save}>
		<div class="mt-4 flex flex-col gap-4">
			<Field id="ename" label="Organization name" bind:value={editName} error={nameError} />
			{#if !topLevel}
				<OrgSelect id="eparent" label="Parent organization" bind:value={editParent} placeholder="Choose a parent" exclude={code} />
			{/if}
		</div>
		{#if editError}<p class="err" role="alert">{editError}</p>{/if}
		<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
			<button type="button" class="btn btn-soft2" onclick={() => (editOpen = false)}>Cancel</button>
			<button type="submit" class="btn btn-primary" disabled={saving || (!topLevel && !editParent)}>Save</button>
		</div>
	</form>
</Dialog>

<Dialog bind:open={deleteOpen} title="Delete organization?">
	<p class="mt-3 text-[16px]">This deletes <strong>{title}</strong> in Knox.</p>
	{#if deleteError}<p class="err" role="alert">{deleteError}</p>{/if}
	<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => (deleteOpen = false)}>Cancel</button>
		<button type="button" class="btn btn-danger" disabled={deleting} onclick={remove}>Delete organization</button>
	</div>
</Dialog>

<Dialog bind:open={moveOpen} title="Move users here">
	<p class="muted mt-1">to {title}</p>
	<div class="mt-4">
		{#if moveLoading}
			<div class="py-8 text-center"><span class="loading loading-spinner"></span></div>
		{:else}
			<SearchPicker
				items={pickable}
				label={(u) => `${displayName(u)} (${u.userId})`}
				placeholder="Search name or user ID"
				onpick={(u) => (chosen = [...chosen, u])}
			/>
		{/if}
	</div>
	{#if chosen.length}
		<h3 class="mt-4 text-[16px] font-semibold">Selected ({chosen.length})</h3>
		<ul>
			{#each chosen as u (u.userId)}
				<li class="row !min-h-12">
					<span class="rowmain"><span class="nm !text-[16px]">{displayName(u)}</span></span>
					<button type="button" class="btn btn-quiet-danger !min-h-11" aria-label="Unselect {displayName(u)}" onclick={() => (chosen = chosen.filter((c) => c !== u))}>Remove</button>
				</li>
			{/each}
		</ul>
	{/if}
	{#if moveError}<p class="err" role="alert">{moveError}</p>{/if}
	<div class="mt-5 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => (moveOpen = false)}>Cancel</button>
		<button type="button" class="btn btn-primary" disabled={!chosen.length} onclick={() => ((moveOpen = false), (moveConfirm = true))}>Move {chosen.length || ''} {chosen.length === 1 ? 'user' : 'users'}</button>
	</div>
</Dialog>

<Dialog bind:open={moveConfirm} title="Move {chosen.length} {chosen.length === 1 ? 'user' : 'users'}?">
	<ul class="mt-3 max-h-[40vh] overflow-y-auto">
		{#each chosen as u (u.userId)}
			<li class="py-1">{displayName(u)}: they will leave {u.orgName || orgs.name(u.orgCode)}</li>
		{/each}
	</ul>
	<div class="mt-5 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => ((moveConfirm = false), (moveOpen = true))}>Cancel</button>
		<button type="button" class="btn btn-primary" disabled={moving} onclick={move}>Move to {title}</button>
	</div>
</Dialog>
