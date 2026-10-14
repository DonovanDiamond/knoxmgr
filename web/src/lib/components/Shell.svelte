<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api } from '#lib/api/client';
	import { logout } from '#lib/api/auth';
	import { session } from '#lib/api/session.svelte';
	import type { Health } from '#lib/api/types';
	import { shell, type ShellAction } from '#lib/shell.svelte';
	import { theme } from '#lib/theme.svelte';
	import Chip from './Chip.svelte';
	import Icon, { type IconName } from './Icon.svelte';
	import Toast from './Toast.svelte';
	import UpdateNotice from './UpdateNotice.svelte';

	// App chrome: sticky header, desktop nav, side panel (md+), phone action bar + tab bar.
	let { children }: { children: Snippet } = $props();

	let menu = $state<HTMLDetailsElement>();
	let name = $state('Knoxmgr');
	let version = $state('');
	onMount(async () => {
		try {
			const h = await api<Health>('/api/health');
			name = h.name;
			version = h.version;
		} catch {
			// Keep the default name; the shell is still usable.
		}
	});

	const tabs = $derived<{ href: string; label: string; icon: IconName }[]>([
		{ href: '/users', label: 'Users', icon: 'users' },
		{ href: '/devices', label: 'Devices', icon: 'device' },
		{ href: '/apps', label: 'Apps', icon: 'apps' },
		{ href: '/groups', label: 'Groups', icon: 'groups' },
		...(session.user?.role === 'admin' ? [{ href: '/admin', label: 'Admin', icon: 'admin' as const }] : [])
	]);
	const current = (href: string) => (page.url.pathname + '/').startsWith(href + '/') || undefined;

	const themeIcon: Record<string, IconName> = { system: 'monitor', light: 'sun', dark: 'moon' };
	const themeNext = { system: 'light', light: 'dark', dark: 'system' } as const;

	const mobileActions = $derived(
		shell.panel && shell.panel.mobile !== false
			? [...shell.panel.actions].sort((a, b) => Number(!!b.lead) - Number(!!a.lead))
			: []
	);
	const kindClass = { primary: 'btn-primary', soft: 'btn-soft2', 'quiet-danger': 'btn-quiet-danger', danger: 'btn-danger', 'outline-danger': 'btn-outline btn-error' };
	const cls = (a: ShellAction) => kindClass[a.kind ?? 'soft'];
</script>

<header class="hdr sticky top-0 z-30">
	<div class="mx-auto flex h-full max-w-[1040px] items-center gap-3 px-4 md:px-8">
		<a href="/users" class="text-primary text-[18px] font-extrabold tracking-tight">{name}</a>
		<span class="hdr-title min-w-0 flex-1 md:hidden" aria-hidden="true">{shell.title}</span>
		<nav class="ml-4 hidden flex-1 gap-1 md:flex" aria-label="Main">
			{#each tabs as t (t.href)}
				<a href={t.href} class="dtab" aria-current={current(t.href) && 'page'}>{t.label}</a>
			{/each}
		</nav>
		<button
			type="button"
			class="btn btn-ghost btn-circle !h-10 !min-h-10 !w-10"
			onclick={() => theme.cycle()}
			aria-label="Theme: {theme.choice}. Switch to {themeNext[theme.choice]}"
		>
			<Icon name={themeIcon[theme.choice]} size={22} />
		</button>
		{#if session.user}
			<details class="dropdown dropdown-end" bind:this={menu}>
				<summary class="btn btn-ghost !h-10 !min-h-10 px-2" aria-label="Account menu">
					<span class="max-w-[8rem] truncate text-[15px]">{session.user.displayName}</span>
				</summary>
				<div class="dropdown-content bg-base-100 z-40 mt-2 w-60 rounded-2xl border p-4" style="border-color: var(--line)">
					<p class="font-semibold">{session.user.displayName}</p>
					<p class="muted text-[14px]">
						{session.user.username} · {session.user.role === 'admin' ? 'Admin' : session.user.access === 'write' ? 'Read and write' : 'Read only'}
					</p>
					<div class="mt-3 flex flex-col">
						<a href="/account" class="row-hover flex min-h-11 items-center" onclick={() => menu?.removeAttribute('open')}>Edit account</a>
						<button type="button" class="row-hover flex min-h-11 items-center text-left" onclick={logout}>Sign out</button>
					</div>
				</div>
			</details>
		{/if}
	</div>
</header>

<main class="mx-auto max-w-[1040px] px-4 pb-48 md:px-8 md:pb-20">
	{#if session.user?.role === 'admin'}<div class="mt-4"><UpdateNotice /></div>{/if}
	<div class={shell.panel ? 'md:grid md:grid-cols-[minmax(0,1fr)_300px] md:gap-12' : ''}>
		<div class="min-w-0">{@render children()}</div>
		{#if shell.panel}
			{@const p = shell.panel}
			<aside class="sticky top-[76px] mt-[96px] hidden self-start md:block" aria-label="Actions">
				<div class="panel">
					{#if p.head && 'title' in p.head}
						<p class="text-[22px] leading-tight font-extrabold tracking-tight">{p.head.title}</p>
					{:else if p.head}
						<p class="text-[40px] leading-none font-extrabold tracking-tight">{p.head.value}</p>
						<p class="muted mt-1">{p.head.label}</p>
					{/if}
					{#if p.stats}
						<ul class="mt-4 space-y-2">
							{#each p.stats as s (s.label)}
								<li class="flex items-center justify-between">
									<Chip tone={s.tone}>{s.label}</Chip>
									<span class="font-semibold">{s.count}</span>
								</li>
							{/each}
						</ul>
					{/if}
					{#if p.note}
						<p class="muted mt-4 flex gap-2 text-[15px]">
							{#if p.lock}<Icon name="lock" size={18} />{/if}<span>{p.note}</span>
						</p>
					{/if}
					<div class="mt-5 flex flex-col gap-3">
						{#each p.actions as a (a.label)}
							<button type="button" class="btn w-full {cls(a)}" disabled={a.disabled} title={a.disabled ? a.reason : undefined} onclick={a.onclick}>{a.label}</button>
						{/each}
					</div>
				</div>
			</aside>
		{/if}
	</div>
	{#if version && session.user}<p class="muted mt-12 text-[13px]">Version {version}</p>{/if}
</main>

<div class="bg-base-100 fixed inset-x-0 bottom-0 z-30 border-t md:hidden" style="border-color: var(--line); padding-bottom: env(safe-area-inset-bottom)">
	{#if mobileActions.length}
		<div class="px-4 pt-3">
			{#if shell.panel?.lock && shell.panel.note}
				<p class="mb-2 flex items-center gap-2 text-[14px] font-medium" style="color: var(--color-error)">
					<Icon name="lock" size={18} />{shell.panel.note}
				</p>
			{/if}
			<div class="flex gap-3 pb-2">
				{#each mobileActions as a (a.label)}
					<button
						type="button"
						class="btn {cls(a)} {a.lead ? 'flex-none !px-5' : 'flex-1 !px-2 !text-[15px]'}"
						disabled={a.disabled}
						title={a.disabled ? a.reason : undefined}
						onclick={a.onclick}>{a.label}</button
					>
				{/each}
			</div>
		</div>
	{/if}
	<nav class="flex px-2" aria-label="Main">
		{#each tabs as t (t.href)}
			<a href={t.href} class="tab" aria-current={current(t.href) && 'page'}><Icon name={t.icon} />{t.label}</a>
		{/each}
	</nav>
</div>

<Toast />
