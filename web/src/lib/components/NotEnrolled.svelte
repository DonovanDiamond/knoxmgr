<script lang="ts">
	import { ApiError } from '#lib/api/client';
	import { inviteUsers } from '#lib/api/knox';
	import type { EnrollmentResult, KnoxUser } from '#lib/api/types';
	import { displayName } from '#lib/perms';
	import { toast } from '#lib/toast.svelte';
	import InviteResultDialog from './InviteResultDialog.svelte';

	let { users, canInvite, oninvited }: { users: KnoxUser[]; canInvite: boolean; oninvited: () => void } = $props();

	const cap = 10;
	let all = $state(false);
	let busy = $state('');
	let open = $state(false);
	let sent = $state<KnoxUser[]>([]);
	let result = $state<EnrollmentResult | null>(null);

	const pending = $derived(users.filter((u) => u.devices.enrolled === 0));
	const shown = $derived(all ? pending : pending.slice(0, cap));

	const invited = (u: KnoxUser) => (u.invitedAt ? `Invited ${new Date(u.invitedAt).toLocaleDateString()}` : 'Never invited');

	async function invite(u: KnoxUser) {
		busy = u.userId;
		try {
			result = await inviteUsers([u.userId]);
			sent = [u];
			open = true;
			oninvited();
		} catch (e) {
			toast.show(e instanceof ApiError ? e.message : 'Invitations were not sent');
		} finally {
			busy = '';
		}
	}
</script>

<h2 class="text-[20px] font-bold">Not enrolled</h2>
<ul class="mt-2">
	{#each shown as u (u.userId)}
		<li class="row row-hover relative">
			<a href="/users/{encodeURIComponent(u.userId)}" class="rowmain after:absolute after:inset-0">
				<span class="nm">{displayName(u)}</span>
				<span class="sub">{invited(u)}</span>
			</a>
			{#if canInvite}
				<button type="button" class="btn btn-soft btn-sm relative z-10" disabled={!!busy} onclick={() => invite(u)}>
					{#if busy === u.userId}<span class="loading loading-spinner loading-xs"></span>{/if}
					Send invite
				</button>
			{/if}
		</li>
	{:else}
		<li class="py-14 text-center">
			<p class="text-[20px] font-semibold">Everyone has a device</p>
			<p class="muted mt-1">Every user in your scope has at least one enrolled device.</p>
		</li>
	{/each}
</ul>
{#if pending.length > cap}
	<button type="button" class="btn btn-ghost btn-sm mt-2" onclick={() => (all = !all)}>
		{all ? 'Show fewer' : `Show all ${pending.length}`}
	</button>
{/if}

<InviteResultDialog bind:open users={sent} {result} />
