<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { createOrg } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import KnoxOffline from '#components/KnoxOffline.svelte';
	import { knoxStatus } from '#lib/health.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Field from '#components/Field.svelte';
	import Icon from '#components/Icon.svelte';
	import Note from '#components/Note.svelte';
	import OrgSelect from '#components/OrgSelect.svelte';
	import { orgs } from '#lib/orgs.svelte';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';

	let loading = $state(true);
	let loadError = $state('');
	let q = $state('');
	const nodes = $derived(orgs.tree());
	const visible = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		return nodes.filter(({ org }) => !needle || `${org.orgName} ${org.orgCode}`.toLowerCase().includes(needle));
	});

	let createOpen = $state(false);
	let code = $state('');
	let name = $state('');
	let parent = $state('');
	let codeError = $state('');
	let nameError = $state('');
	let formError = $state('');
	let creating = $state(false);

	const isAdmin = $derived(session.user?.role === 'admin');
	const adminOnly = 'Only admins create, edit or delete organizations.';

	async function load() {
		loading = true;
		loadError = '';
		try {
			await orgs.load();
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
		code = '';
		name = '';
		parent = orgs.tree()[0]?.org.orgCode ?? '';
		codeError = nameError = formError = '';
		createOpen = true;
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		const c = code.trim();
		const n = name.trim();
		codeError = !c ? 'Enter an organization code.' : orgs.list.some((o) => o.orgCode === c) ? `The code "${c}" is already used.` : '';
		nameError = n ? '' : 'Enter an organization name.';
		formError = '';
		if (codeError || nameError) return;
		creating = true;
		try {
			await createOrg({ orgCode: c, orgName: n, parentCode: parent });
			createOpen = false;
			toast.show(`Created ${n}`);
			await orgs.refresh();
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Could not reach the server.';
		} finally {
			creating = false;
		}
	}

	usePanel(() => ({
		head: { value: orgs.list.length, label: orgs.list.length === 1 ? 'organization' : 'organizations' },
		note: isAdmin ? '' : adminOnly,
		lock: !isAdmin,
		actions: isAdmin ? [{ label: 'New organization', kind: 'primary', onclick: openCreate }] : []
	}));
</script>

<BigTitle title="Orgs" />

{#if !isAdmin && session.user}
	<div class="mb-4"><Note tone="warn" icon="lock">{adminOnly} You can open an organization to see its members.</Note></div>
{/if}

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if knoxStatus.configured === false}
	<KnoxOffline />
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load organizations</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<div class="relative mb-2">
		<span class="muted absolute top-1/2 left-3 -translate-y-1/2"><Icon name="search" size={20} /></span>
		<label class="sr-only" for="q">Search organizations</label>
		<input id="q" type="search" class="field !pl-10" placeholder="Search name or code" autocomplete="off" bind:value={q} />
	</div>
	<ul>
		{#each visible as { org, depth } (org.orgCode)}
			<li class="row row-hover">
				<a href="/orgs/{encodeURIComponent(org.orgCode)}" class="rowmain !py-3" style="padding-left: {q.trim() ? 0 : depth * 1.5}rem">
					<span class="nm !text-[20px]">{org.orgName}</span>
					<span class="sub">{org.orgCode}</span>
				</a>
				<span class="muted"><Icon name="chev" size={24} /></span>
			</li>
		{:else}
			<li class="py-14 text-center">
				<p class="text-[20px] font-semibold">{orgs.list.length ? 'No organization matches that search' : 'No organizations'}</p>
				<p class="muted mt-1">{orgs.list.length ? 'Check the spelling.' : isAdmin ? 'Create an organization to start.' : adminOnly}</p>
			</li>
		{/each}
	</ul>
{/if}

<Dialog bind:open={createOpen} title="New organization">
	<form novalidate onsubmit={create}>
		<div class="mt-4 flex flex-col gap-4">
			<Field id="ocode" label="Organization code" bind:value={code} error={codeError} hint="Cannot be changed later." />
			<Field id="oname" label="Organization name" bind:value={name} error={nameError} />
			<OrgSelect id="oparent" label="Parent organization" bind:value={parent} placeholder="Choose a parent" />
		</div>
		{#if formError}<p class="err" role="alert">{formError}</p>{/if}
		<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
			<button type="button" class="btn btn-soft2" onclick={() => (createOpen = false)}>Cancel</button>
			<button type="submit" class="btn btn-primary" disabled={creating || !parent}>Create organization</button>
		</div>
	</form>
</Dialog>
