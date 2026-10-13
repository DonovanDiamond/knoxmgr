export type ThemeChoice = 'system' | 'light' | 'dark';

const names = { light: 'fclight', dark: 'fcdark' } as const;
const next: Record<ThemeChoice, ThemeChoice> = { system: 'light', light: 'dark', dark: 'system' };

function read(): ThemeChoice {
	try {
		const stored = localStorage.getItem('theme');
		if (stored === 'light' || stored === 'dark') return stored;
	} catch {
		// Storage blocked: fall back to following the system.
	}
	return 'system';
}

class ThemeState {
	choice = $state<ThemeChoice>(read());

	/** Steps system, light, dark. "system" clears data-theme so the daisyUI prefersdark rule applies. */
	cycle() {
		this.choice = next[this.choice];
		try {
			if (this.choice === 'system') localStorage.removeItem('theme');
			else localStorage.setItem('theme', this.choice);
		} catch {
			// The choice still applies for this page view.
		}
		if (this.choice === 'system') delete document.documentElement.dataset.theme;
		else document.documentElement.dataset.theme = names[this.choice];
	}
}

export const theme = new ThemeState();
