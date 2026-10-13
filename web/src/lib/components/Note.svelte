<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		tone = 'info',
		icon = 'info',
		children
	}: { tone?: 'info' | 'warn'; icon?: 'info' | 'lock'; children: Snippet } = $props();
</script>

<div class="note" class:warn={tone === 'warn'} role={tone === 'warn' ? 'alert' : 'status'}>
	<Icon name={icon} size={18} />
	<span>{@render children()}</span>
</div>
