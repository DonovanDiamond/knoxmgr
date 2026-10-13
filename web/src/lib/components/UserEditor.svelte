<script lang="ts">
	import { goto } from '$app/navigation';
	import { ApiError } from '#lib/api/client';
	import { createUser, getUser, updateUser } from '#lib/api/knox';
	import { session } from '#lib/api/session.svelte';
	import type { KnoxUser, KnoxUserFields } from '#lib/api/types';
	import { orgs } from '#lib/orgs.svelte';
	import { accessOf } from '#lib/perms';
	import { usePanel } from '#lib/shell.svelte';
	import { toast } from '#lib/toast.svelte';
	import BigTitle from './BigTitle.svelte';
	import Field from './Field.svelte';
	import Note from './Note.svelte';
	import OrgSelect from './OrgSelect.svelte';

	// Add (no id) or edit (id) a Knox user. Owns loading, validation and saving.
	// ondone/oncancel default to returning to the user list.
	let { id, ondone, oncancel }: { id?: string; ondone?: () => void; oncancel?: () => void } = $props();

	const emptyVals = () => ({
		userId: '', userName: '', firstName: '', lastName: '', email: '', contact: '', empNo: '', orgCode: '', password: ''
	});
	type Vals = ReturnType<typeof emptyVals>;

	let vals = $state<Vals>(emptyVals());
	let original: Vals | null = null;
	let errors = $state<Partial<Record<keyof Vals, string>>>({});
	let requestEnrollment = $state(false);
	let loading = $state(true);
	let loadError = $state('');
	let formError = $state('');
	let busy = $state(false);
	// While false, the display name follows "First Last"; typing in it ends that.
	let nameEdited = $state(false);
	const fullName = (v: { firstName: string; lastName: string }) => `${v.firstName} ${v.lastName}`.trim();

	const access = $derived(accessOf(session.user, orgs.name(session.user?.orgCode)));
	const editing = $derived(!!id);
	const disabled = $derived(!access.canWrite || busy);

	$effect(() => {
		orgs.load().catch(() => {});
	});

	$effect(() => {
		if (!id) {
			vals.orgCode ||= access.scopeOrg ?? '';
			loading = false;
			return;
		}
		loading = true;
		getUser(id)
			.then((u: KnoxUser) => {
				vals = { ...emptyVals(), userId: u.userId, userName: u.userName, firstName: u.firstName, lastName: u.lastName, email: u.email, contact: u.contact, empNo: u.empNo, orgCode: u.orgCode };
				original = { ...vals };
				nameEdited = vals.userName.trim() !== fullName(vals);
			})
			.catch((e: unknown) => {
				loadError =
					e instanceof ApiError && e.status === 404
						? `This user does not exist, or is outside your organization${access.scopeOrg ? ` (${orgs.name(access.scopeOrg)} and its sub-organizations)` : ''}.`
						: e instanceof ApiError ? e.message : 'Could not reach the server.';
			})
			.finally(() => (loading = false));
	});

	function setName(part: 'firstName' | 'lastName', value: string) {
		vals[part] = value;
		if (!nameEdited) vals.userName = fullName(vals);
	}

	function validate(): typeof errors {
		const e: typeof errors = {};
		const v = vals;
		if (!editing && !/^[A-Za-z0-9_.@+:-]{1,50}$/.test(v.userId))
			e.userId = 'User ID must be 1 to 50 letters, numbers, dots, dashes or underscores. Remove spaces.';
		if (!v.userName.trim()) e.userName = 'Enter the name to show in Knox, for example Thandi Mokoena.';
		for (const k of ['userName', 'firstName', 'lastName', 'email', 'contact', 'empNo'] as const)
			if (v[k].length > 100) e[k] = 'Use at most 100 characters.';
		if (v.email && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(v.email.trim()))
			e.email = 'This email is missing a domain. Use the form name@example.org.';
		if (!v.orgCode) e.orgCode = 'Choose an organization.';
		if (v.password.length > 128) e.password = 'Use at most 128 characters.';
		return e;
	}

	async function save() {
		if (disabled) return;
		formError = '';
		errors = validate();
		if (Object.keys(errors).length) {
			document.querySelector<HTMLElement>('[aria-invalid=true]')?.focus();
			return;
		}
		const v = Object.fromEntries(Object.entries(vals).map(([k, x]) => [k, x.trim()])) as Vals;
		busy = true;
		try {
			if (id) {
				const fields: KnoxUserFields = {};
				for (const k of ['userName', 'firstName', 'lastName', 'email', 'contact', 'empNo', 'orgCode'] as const) {
					// The API treats an empty field as "leave unchanged", so only non-empty edits are sent.
					if (v[k] && v[k] !== original?.[k]) fields[k] = v[k];
				}
				if (Object.keys(fields).length) await updateUser(id, fields);
				toast.show(Object.keys(fields).length ? `Saved ${v.userName}` : 'No changes to save');
			} else {
				await createUser({
					userId: v.userId, userName: v.userName, firstName: v.firstName, lastName: v.lastName, email: v.email,
					contact: v.contact, empNo: v.empNo, orgCode: v.orgCode, password: v.password || undefined,
					requestEnrollment: requestEnrollment && !!v.email
				});
				toast.show(`Added ${v.userName}`);
			}
			await (ondone ?? (() => goto('/users')))();
		} catch (e) {
			formError = e instanceof ApiError ? e.message : 'Could not reach the server.';
		} finally {
			busy = false;
		}
	}

	const cancel = () => (oncancel ?? (() => goto('/users')))();

	usePanel(() => ({
		head: { title: editing ? `Editing ${id}` : 'New user' },
		note: access.canWrite ? 'Saves to Knox right away.' : access.reason,
		lock: !access.canWrite,
		mobile: false,
		actions: [
			{ label: 'Save user', kind: 'primary', onclick: save, disabled: disabled || loading || !!loadError, reason: access.reason },
			{ label: 'Cancel', kind: 'soft', onclick: cancel }
		]
	}));
</script>

<BigTitle title={editing ? 'Edit user' : 'Add user'} back={editing ? `/users/${encodeURIComponent(id ?? '')}` : '/users'} />

{#if loading}
	<div class="py-14 text-center"><span class="loading loading-spinner loading-lg"></span></div>
{:else if loadError}
	<Note tone="warn" icon="lock">{loadError}</Note>
	<a href="/users" class="btn btn-soft2 mt-5">Back to users</a>
{:else}
	{#if !access.canWrite}
		<div class="mb-5 max-w-[34rem]"><Note tone="warn" icon="lock"><strong>{access.reason}.</strong> You can view this person but not change them.</Note></div>
	{/if}
	<form class="max-w-[34rem] space-y-5" novalidate onsubmit={(e) => { e.preventDefault(); save(); }}>
		<fieldset {disabled} class="space-y-5">
			<div class="grid gap-5 sm:grid-cols-2">
				<Field id="f-firstName" label="First name" value={vals.firstName} oninput={(v) => setName('firstName', v)} error={errors.firstName} />
				<Field id="f-lastName" label="Last name" value={vals.lastName} oninput={(v) => setName('lastName', v)} error={errors.lastName} />
			</div>
			<Field id="f-userName" label="Display name" bind:value={vals.userName} oninput={() => (nameEdited = true)} error={errors.userName}
				hint="Filled in from the first and last name. You can change it." />
			<Field id="f-userId" label="User ID" bind:value={vals.userId} error={errors.userId} readonly={editing}
				hint={editing ? 'The user ID cannot be changed after the user is created.' : 'Letters, numbers, dots and dashes. For example thandi.mokoena.'} />
			<Field id="f-email" label="Email" type="email" inputmode="email" bind:value={vals.email} error={errors.email} hint="The enrollment invitation is sent to this address." />
			<div class="grid gap-5 sm:grid-cols-2">
				<Field id="f-contact" label="Contact number" type="tel" inputmode="tel" bind:value={vals.contact} error={errors.contact} />
				<Field id="f-empNo" label="Employee number" inputmode="numeric" bind:value={vals.empNo} error={errors.empNo} />
			</div>
			<OrgSelect id="f-orgCode" label="Organization" bind:value={vals.orgCode} root={access.scopeOrg} placeholder="Choose an organization" error={errors.orgCode} />
			{#if !editing}
				<Field id="f-password" label="Initial password (optional)" type="password" autocomplete="new-password" bind:value={vals.password} error={errors.password} />
				<label class="flex min-h-12 items-center gap-3">
					<input type="checkbox" class="checkbox checkbox-primary" bind:checked={requestEnrollment} disabled={!vals.email.trim()} />
					<span>Send the enrollment invitation now{vals.email.trim() ? '' : ' (needs an email)'}</span>
				</label>
			{/if}
		</fieldset>
		{#if formError}<div role="alert" class="note warn"><span>{formError}</span></div>{/if}
		<button class="btn btn-primary w-full md:hidden" type="submit" {disabled}>Save user</button>
		<button type="button" class="btn btn-soft2 w-full md:hidden" onclick={cancel}>Cancel</button>
	</form>
{/if}
