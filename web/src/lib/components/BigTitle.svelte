<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import { shell } from '#lib/shell.svelte';

	// Page title. Once it scrolls under the header, the header shows a small copy instead.
	let { title, back = null, actions }: { title: string; back?: string | null; actions?: Snippet } = $props();
	let el: HTMLHeadingElement;

	$effect(() => {
		shell.title = title;
		shell.back = back;
	});

	onMount(() => {
		const io = new IntersectionObserver(([e]) => (shell.scrolled = !e.isIntersecting), {
			rootMargin: '-56px 0px 0px 0px'
		});
		io.observe(el);
		return () => {
			io.disconnect();
			shell.scrolled = false;
			shell.title = '';
			shell.back = null;
		};
	});
</script>

<div class="flex flex-wrap items-center gap-x-2">
	{#if shell.back}
		<a
			href={shell.back}
			onclick={(e) => {
				if (shell.depth <= 0) return;
				e.preventDefault();
				history.back();
			}}
			class="btn btn-ghost btn-circle -ml-2 flex-none" aria-label="Back"><Icon name="back" size={28} /></a>
	{/if}
	<h1 bind:this={el} class="big min-w-0">{title}</h1>
	{#if actions}<div class="ml-auto flex flex-wrap items-center gap-2">{@render actions()}</div>{/if}
</div>
