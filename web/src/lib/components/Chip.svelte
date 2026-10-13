<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { ChipTone } from '#lib/shell.svelte';

	let { tone = 'plain', children }: { tone?: ChipTone; children: Snippet } = $props();
	const cls: Record<ChipTone, string> = {
		noemail: 's-noemail',
		plain: 'plain'
	};
</script>

<span class="chip {cls[tone]}">{@render children()}</span>
