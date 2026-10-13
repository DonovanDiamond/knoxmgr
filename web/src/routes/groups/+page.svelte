<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { createGroup, listGroups } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import type { Group } from '#lib/api/types';
	import KnoxOffline from '#components/KnoxOffline.svelte';
	import { knoxStatus } from '#lib/health.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Field from '#components/Field.svelte';
	import Icon from '#components/Icon.svelte';
	import Note from '#components/Note.svelte';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';

	let groups = $state<Group[]>([]);
	let loading = $state(true);
	let loadError = $state('');
	let q = $state('');
	const visible = $derived(groups.filter((g) => g.groupName.toLowerCase().includes(q.trim().toLowerCase())));

	let createOpen = $state(false);
	let newName = $state('');
	let nameError = $state('');
	let creating = $state(false);

	const isAdmin = $derived(session.user?.role === 'admin');
	const adminOnly = 'Only admins create or delete groups.';

	async function load() {
		loading = true;
		loadError = '';
		try {
			groups = await listGroups();
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	function openCreate() {
		newName = '';
		nameError = '';
		createOpen = true;
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		const name = newName.trim();
		if (!name) nameError = 'Enter a group name.';
		else if (groups.some((g) => g.groupName.toLowerCase() === name.toLowerCase()))
			nameError = `A group called "${name}" already exists. Use a different name.`;
		else nameError = '';
		if (nameError) return;
		creating = true;
		try {
			await createGroup(name);
			createOpen = false;
			toast.show(`Created ${name}`);
			groups = await listGroups();
		} catch (err) {
			nameError = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			creating = false;
		}
	}

	usePanel(() => ({
		head: { value: groups.length, label: groups.length === 1 ? 'group' : 'groups' },
		note: isAdmin ? '' : adminOnly,
		lock: !isAdmin,
		actions: isAdmin ? [{ label: 'Create group', kind: 'primary', onclick: openCreate }] : []
	}));
</script>

<BigTitle title="Groups" />

{#if !isAdmin && session.user}
	<div class="mb-4"><Note tone="warn" icon="lock">{adminOnly} You can open a group to see its members.</Note></div>
{/if}

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if knoxStatus.configured === false}
	<KnoxOffline />
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load groups</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<div class="relative mb-2">
		<span class="muted absolute top-1/2 left-3 -translate-y-1/2"><Icon name="search" size={20} /></span>
		<label class="sr-only" for="q">Search groups</label>
		<input id="q" type="search" class="field !pl-10" placeholder="Search group name" autocomplete="off" bind:value={q} />
	</div>
	<ul>
		{#each visible as g (g.groupId)}
			<li class="row row-hover">
				<a href="/groups/{encodeURIComponent(g.groupId)}" class="rowmain !py-3">
					<span class="nm !text-[20px]">{g.groupName}</span>
					<span class="sub">{g.userCount} {g.userCount === 1 ? 'member' : 'members'}</span>
				</a>
				<span class="muted"><Icon name="chev" size={24} /></span>
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">{groups.length ? 'No group matches that search' : 'No groups yet'}</p>
				<p class="muted mt-1">{groups.length ? 'Check the spelling.' : isAdmin ? 'Create a group to start organizing people.' : adminOnly}</p>
			</li>
		{/each}
	</ul>
{/if}

<Dialog bind:open={createOpen} title="Create group">
	<p class="muted mt-1">Groups cannot be renamed in Knox, so check the spelling.</p>
	<form novalidate onsubmit={create}>
		<div class="mt-4"><Field id="gname" label="Group name" bind:value={newName} error={nameError} /></div>
		<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
			<button type="button" class="btn btn-soft2" onclick={() => (createOpen = false)}>Cancel</button>
			<button type="submit" class="btn btn-primary" disabled={creating}>Create group</button>
		</div>
	</form>
</Dialog>
