import type { AuthResponse, User } from './types';

// Access token lives in memory only; the refresh cookie restores it after a reload.
class Session {
	token = $state<string | null>(null);
	user = $state<User | null>(null);

	set(r: AuthResponse) {
		this.token = r.accessToken;
		this.user = r.user;
	}

	clear() {
		this.token = null;
		this.user = null;
	}
}

export const session = new Session();
