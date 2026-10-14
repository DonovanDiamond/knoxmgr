<script lang="ts">
	import { changePassword, updateMe } from '#lib/api/account';
	import { ApiError } from '#lib/api/client';
	import { session } from '#lib/api/session.svelte';
	import BigTitle from '#components/BigTitle.svelte';
	import Field from '#components/Field.svelte';
	import Note from '#components/Note.svelte';
	import { toast } from '#lib/toast.svelte';

	// Keep in step with MinPasswordLen / MaxPasswordLen in src/config/config.go.
	const MIN_PASSWORD = 8;
	const MAX_PASSWORD = 72;

	const user = $derived(session.user);
	const isAd = $derived(user?.authSource === 'ad');

	const messages: Record<string, string> = {
		ad_managed: 'This account is managed in Active Directory. Change it there.',
		wrong_password: 'The current password is not correct.',
		invalid_password: `The new password must be ${MIN_PASSWORD} to ${MAX_PASSWORD} characters.`
	};
	const explain = (e: unknown) =>
		e instanceof ApiError
			? ((e.code && messages[e.code]) ?? (e.status === 429 ? 'Too many attempts. Wait a few minutes and try again.' : e.message))
			: 'Could not reach the server.';

	let displayName = $state(session.user?.displayName ?? '');
	let nameError = $state('');
	let savingName = $state(false);

	async function saveName(e: SubmitEvent) {
		e.preventDefault();
		const n = displayName.trim();
		if (!n) {
			nameError = 'Enter a name';
			return;
		}
		savingName = true;
		nameError = '';
		try {
			const u = await updateMe(n);
			session.user = u;
			displayName = u.displayName;
			toast.show('Account updated');
		} catch (err) {
			nameError = explain(err);
		} finally {
			savingName = false;
		}
	}

	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let pwErrors = $state<Record<string, string>>({});
	let pwFormError = $state('');
	let savingPw = $state(false);

	async function savePassword(e: SubmitEvent) {
		e.preventDefault();
		const v: Record<string, string> = {};
		if (!current) v.current = 'Enter your current password';
		if (next.length < MIN_PASSWORD || next.length > MAX_PASSWORD) v.next = `Use ${MIN_PASSWORD} to ${MAX_PASSWORD} characters`;
		if (confirm !== next) v.confirm = 'Passwords do not match';
		pwErrors = v;
		pwFormError = '';
		if (Object.keys(v).length) return;
		savingPw = true;
		try {
			await changePassword(current, next);
			current = next = confirm = '';
			toast.show('Password changed');
		} catch (err) {
			pwFormError = explain(err);
		} finally {
			savingPw = false;
		}
	}

	const roleLabel = $derived(user?.role === 'admin' ? 'Admin' : user?.access === 'write' ? 'User, read and write' : 'User, read only');
</script>

<BigTitle title="Edit account" />

{#if user}
	{#if isAd}
		<Note icon="lock">Your details and password are managed in Active Directory. Change them there.</Note>
	{/if}

	<form class="mt-4 flex max-w-xl flex-col gap-4" onsubmit={saveName}>
		<Field id="a-username" label="Username" value={user.username} readonly />
		<Field id="a-role" label="Role" value={roleLabel} readonly />
		{#if user.role !== 'admin'}<Field id="a-org" label="Organization (and its sub-organizations)" value={user.orgCode ?? ''} readonly />{/if}
		<Field id="a-source" label="Sign-in source" value={isAd ? 'Active Directory' : 'Local account'} readonly />
		<Field id="a-name" label="Display name" bind:value={displayName} readonly={isAd} error={nameError} autocomplete="name" />
		{#if !isAd}
			<div class="mt-2 flex sm:justify-end">
				<button type="submit" class="btn btn-primary w-full sm:w-auto" disabled={savingName}>{savingName ? 'Saving...' : 'Save'}</button>
			</div>
		{/if}
	</form>

	{#if !isAd}
		<form class="mt-10 flex max-w-xl flex-col gap-4" onsubmit={savePassword}>
			<h2 class="text-[20px] font-semibold">Change password</h2>
			<Field id="a-current" label="Current password" type="password" bind:value={current} error={pwErrors.current ?? ''} autocomplete="current-password" />
			<Field
				id="a-new"
				label="New password"
				type="password"
				bind:value={next}
				error={pwErrors.next ?? ''}
				hint="{MIN_PASSWORD} to {MAX_PASSWORD} characters."
				autocomplete="new-password"
			/>
			<Field id="a-confirm" label="Confirm new password" type="password" bind:value={confirm} error={pwErrors.confirm ?? ''} autocomplete="new-password" />
			{#if pwFormError}<p class="err" role="alert">{pwFormError}</p>{/if}
			<div class="mt-2 flex sm:justify-end">
				<button type="submit" class="btn btn-primary w-full sm:w-auto" disabled={savingPw}>{savingPw ? 'Changing...' : 'Change password'}</button>
			</div>
		</form>
	{/if}
{/if}
