import { onDestroy } from 'svelte';

export type ChipTone = 'noemail' | 'plain';

export interface ShellAction {
	label: string;
	onclick: () => void;
	kind?: 'primary' | 'soft' | 'quiet-danger';
	disabled?: boolean;
	/** Tooltip on a disabled action: why it is unavailable. */
	reason?: string;
	/** On phones this action sits first and keeps its natural width; the rest share the space. */
	lead?: boolean;
}

export interface PanelSpec {
	/** Big number plus caption, e.g. {value: 3, label: 'selected'}; or a plain title. */
	head?: { value: string | number; label: string } | { title: string };
	stats?: { tone: ChipTone; label: string; count: number }[];
	note?: string;
	/** Draws the note with a lock icon: it explains a read-only state. */
	lock?: boolean;
	actions: ShellAction[];
	/** Show the actions in the phone bottom bar (default true). */
	mobile?: boolean;
}

class Shell {
	/** Collapsed title shown in the header once the BigTitle has scrolled away. */
	title = $state('');
	scrolled = $state(false);
	/** Href of the back arrow shown beside the BigTitle; null hides it. */
	back = $state<string | null>(null);
	panel = $state<PanelSpec | null>(null);
	/** In-app history entries behind the current page; history.back() stays in the app only while > 0. */
	depth = $state(0);
}

export const shell = new Shell();

/** Registers the side panel / phone action bar for the current page. Call during component init. */
export function usePanel(spec: () => PanelSpec) {
	$effect(() => {
		shell.panel = spec();
	});
	onDestroy(() => (shell.panel = null));
}
