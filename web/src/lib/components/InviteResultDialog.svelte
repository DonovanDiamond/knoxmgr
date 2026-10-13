<script lang="ts">
	import type { EnrollmentResult, KnoxUser } from '#lib/api/types';
	import { displayName } from '#lib/perms';
	import Avatar from './Avatar.svelte';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';

	// Outcome of POST /users/invite. users are the people that were sent in the request.
	let {
		open = $bindable(false),
		users,
		result
	}: { open?: boolean; users: KnoxUser[]; result: EnrollmentResult | null } = $props();

	const noMail = $derived(users.filter((u) => result?.userIdWithoutEmail?.includes(u.userId)));
	const missing = $derived(result?.notExistUserId ?? []);
	const sent = $derived(users.filter((u) => !noMail.includes(u) && !missing.includes(u.userId)));
</script>

<Dialog bind:open title="{sent.length} {sent.length === 1 ? 'invite' : 'invites'} sent">
	{#snippet lead()}
		<span class="mb-3 grid h-11 w-11 place-items-center rounded-full" style="background: var(--color-success); color: var(--color-success-content)">
			<Icon name="check" />
		</span>
	{/snippet}
	<p class="muted mt-1">People get an email with the enrollment link for their phone.</p>
	<div class="mt-4 max-h-[40vh] overflow-y-auto">
		<ul>
			{#each sent as u (u.userId)}
				<li class="flex items-center gap-3 py-2">
					<Avatar name={displayName(u)} seed={u.userId} small />
					<span class="rowmain">
						<span class="nm !text-[16px]">{displayName(u)}</span>
						<span class="sub">{u.email}</span>
					</span>
				</li>
			{/each}
		</ul>
		{#if noMail.length || missing.length}
			<div class="note warn mt-3">
				<span>
					<strong>Not invited</strong>
					{#if noMail.length}<br />No email address: {noMail.map((u) => `${displayName(u)} (${u.userId})`).join(', ')}. Add an email, then invite again.{/if}
					{#if missing.length}<br />Not found in Knox: {missing.join(', ')}. Check the user ID.{/if}
				</span>
			</div>
		{/if}
	</div>
	<button type="button" class="btn btn-primary mt-6 w-full" onclick={() => (open = false)}>Done</button>
</Dialog>
