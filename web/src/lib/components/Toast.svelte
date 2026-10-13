<script lang="ts">
	import { toast } from '#lib/toast.svelte';
</script>

<div class="toast toast-top toast-center pointer-events-none z-50 !top-16" aria-live="polite">
	{#if toast.message}<div class="toast-msg">{toast.message}</div>{/if}
</div>
