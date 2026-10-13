<script lang="ts">
	// Offline monogram tile; the hue is hashed from the package so one app always looks the same.
	let { name, pkg, size = 'md' }: { name: string; pkg: string; size?: 'sm' | 'md' | 'lg' } = $props();

	const letters = $derived(
		(name || pkg)
			.split(/[\s._-]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((w) => [...w][0])
			.join('')
			.toUpperCase() || '?'
	);
	const hue = $derived([...pkg].reduce((n, c) => (n * 31 + c.charCodeAt(0)) % 360, 7));
	const dim = { sm: 'h-8 w-8 text-[13px]', md: 'h-11 w-11 text-[16px]', lg: 'h-16 w-16 text-[24px]' };
</script>

<span
	class="inline-grid flex-none place-items-center rounded-xl font-extrabold {dim[size]}"
	style="background: oklch(0.9 0.06 {hue}); color: oklch(0.38 0.1 {hue})"
	aria-hidden="true">{letters}</span
>
