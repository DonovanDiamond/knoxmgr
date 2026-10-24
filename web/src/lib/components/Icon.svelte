<script lang="ts" module>
	export type IconName =
		| 'sun' | 'moon' | 'monitor' | 'users' | 'groups' | 'admin' | 'lock' | 'info' | 'check' | 'chev' | 'search' | 'back' | 'device' | 'apps' | 'orgs' | 'home';
</script>

<script lang="ts">
	let { name, size = 24 }: { name: IconName; size?: number } = $props();

	const paths: Record<IconName, string> = {
		sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
		moon: '<path d="M21 13A9 9 0 1111 3a7 7 0 0010 10z"/>',
		monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
		users: '<circle cx="9" cy="8" r="4"/><path d="M2 21c0-4 3-6 7-6s7 2 7 6M17 4a4 4 0 010 8M22 21c0-3-1.5-5-4-5.7"/>',
		groups: '<rect x="3" y="3" width="8" height="8" rx="2"/><rect x="13" y="3" width="8" height="8" rx="2"/><rect x="3" y="13" width="8" height="8" rx="2"/><rect x="13" y="13" width="8" height="8" rx="2"/>',
		admin: '<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/><path d="M9 12l2 2 4-4"/>',
		lock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 018 0v4"/>',
		info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
		check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
		chev: '<path d="M9 5l7 7-7 7"/>',
		search: '<circle cx="11" cy="11" r="7"/><path d="M20 20l-4-4"/>',
		back: '<path d="M15 5l-7 7 7 7"/>',
		device: '<rect x="7" y="2" width="10" height="20" rx="2.5"/><path d="M11 18h2"/>',
		orgs: '<rect x="9" y="3" width="6" height="5" rx="1.5"/><rect x="2" y="16" width="6" height="5" rx="1.5"/><rect x="16" y="16" width="6" height="5" rx="1.5"/><path d="M12 8v4M5 16v-4h14v4"/>',
		home: '<path d="M3 11l9-8 9 8"/><path d="M5 10v10h14V10M10 20v-6h4v6"/>',
		apps: '<rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><path d="M17.5 14v7M14 17.5h7"/>'
	};
</script>

<!-- Paths are the fixed literals above, never user input. -->
<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width={name === 'check' ? 3 : 2.2}
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
>
	{@html paths[name]}
</svg>
