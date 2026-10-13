<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { createAppCategory, deleteAppCategory, listAppCategories, renameAppCategory, type AppCategory } from '#lib/api/apps';
	import { session } from '#lib/api/session.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Dialog from '#components/Dialog.svelte';
	import Field from '#components/Field.svelte';
	import Note from '#components/Note.svelte';
	import { toast } from '#lib/toast.svelte';

	const isAdmin = $derived(session.user?.role === 'admin');

	let categories = $state<AppCategory[]>([]);
	let loading = $state(true);
	let loadError = $state('');

	async function load() {
		loading = true;
		loadError = '';
		try {
			categories = (await listAppCategories()).categories;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		if (isAdmin) load();
	});

	const msg = (e: unknown) => (e instanceof ApiError ? e.message : 'Could not reach the server.');

	let newName = $state('');
	let addError = $state('');
	let adding = $state(false);
	async function add(e: SubmitEvent) {
		e.preventDefault();
		const name = newName.trim();
		if (!name) {
			addError = 'Enter a name';
			return;
		}
		adding = true;
		addError = '';
		try {
			await createAppCategory(name);
			newName = '';
			toast.show('Category added');
			await load();
		} catch (err) {
			addError = msg(err);
		} finally {
			adding = false;
		}
	}

	let editId = $state<number | null>(null);
	let editName = $state('');
	let editError = $state('');
	let saving = $state(false);
	function startEdit(c: AppCategory) {
		editId = c.id;
		editName = c.name;
		editError = '';
	}
	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		const name = editName.trim();
		if (editId === null) return;
		if (!name) {
			editError = 'Enter a name';
			return;
		}
		saving = true;
		editError = '';
		try {
			await renameAppCategory(editId, name);
			editId = null;
			toast.show('Category renamed');
			await load();
		} catch (err) {
			editError = msg(err);
		} finally {
			saving = false;
		}
	}

	let target = $state<AppCategory | null>(null);
	let deleteOpen = $state(false);
	let deleting = $state(false);
	let deleteError = $state('');
	function askDelete(c: AppCategory) {
		target = c;
		deleteError = '';
		deleteOpen = true;
	}
	async function remove() {
		if (!target) return;
		deleting = true;
		deleteError = '';
		try {
			await deleteAppCategory(target.id);
			deleteOpen = false;
			toast.show('Category deleted');
			await load();
		} catch (err) {
			deleteError = msg(err);
		} finally {
			deleting = false;
		}
	}
</script>

<BigTitle title="App categories" back="/admin" />

{#if !isAdmin}
	<Note tone="warn" icon="lock"><strong>Admins only.</strong> Settings need an administrator account.</Note>
{:else if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<div class="py-14 text-center">
		<p class="text-[20px] font-semibold">Could not load categories</p>
		<p class="muted mt-1">{loadError}</p>
		<button type="button" class="btn btn-primary mt-5" onclick={load}>Try again</button>
	</div>
{:else}
	<div class="max-w-xl">
		<p class="muted mb-4">Apps on devices are grouped by these categories. The System and Other categories are built in and cannot be edited.</p>
		<form class="mb-6 flex flex-col gap-3 sm:flex-row sm:items-start" onsubmit={add}>
			<div class="flex-1"><Field id="cat-new" label="New category" bind:value={newName} error={addError} /></div>
			<button type="submit" class="btn btn-primary sm:mt-8" disabled={adding}>Add category</button>
		</form>
		<ul>
			{#each categories as c (c.id)}
				<li class="row !items-start">
					{#if editId === c.id}
						<form class="flex w-full flex-col gap-3" onsubmit={saveEdit}>
							<Field id="cat-edit" label="Category name" bind:value={editName} error={editError} />
							<div class="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
								<button type="button" class="btn btn-soft2" onclick={() => (editId = null)}>Cancel</button>
								<button type="submit" class="btn btn-primary" disabled={saving}>Save</button>
							</div>
						</form>
					{:else}
						<span class="rowmain">
							<span class="nm !whitespace-normal break-words">{c.name}{#if c.builtIn} <span class="badge align-middle">Built-in</span>{/if}</span>
							<span class="sub">{c.appCount} {c.appCount === 1 ? 'app' : 'apps'}</span>
						</span>
						{#if !c.builtIn}
							<span class="flex flex-none flex-wrap justify-end gap-2">
								<button type="button" class="btn btn-soft2" onclick={() => startEdit(c)} aria-label="Rename {c.name}">Rename</button>
								<button type="button" class="btn btn-quiet-danger" onclick={() => askDelete(c)} aria-label="Delete {c.name}">Delete</button>
							</span>
						{/if}
					{/if}
				</li>
			{:else}
				<li class="py-14 text-center"><p class="text-[20px] font-semibold">No categories yet</p></li>
			{/each}
		</ul>
	</div>
{/if}

<Dialog bind:open={deleteOpen} title="Delete {target?.name ?? 'category'}?">
	<p class="mt-3 text-[16px] break-words">
		{target?.appCount ? `Its ${target.appCount} ${target.appCount === 1 ? 'app falls' : 'apps fall'} back to Other.` : 'No apps use this category.'}
	</p>
	{#if deleteError}<p class="err" role="alert">{deleteError}</p>{/if}
	<div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
		<button type="button" class="btn btn-soft2" onclick={() => (deleteOpen = false)}>Cancel</button>
		<button type="button" class="btn btn-danger" disabled={deleting} onclick={remove}>Delete category</button>
	</div>
</Dialog>
