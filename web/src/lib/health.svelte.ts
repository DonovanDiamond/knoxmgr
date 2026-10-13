// Whether Knox is connected. Updated from /api/health, any 503 knox_not_configured, and a saved Knox form.
class KnoxStatus {
	/** null until known. */
	configured = $state<boolean | null>(null);
}

export const knoxStatus = new KnoxStatus();
