import type { User } from './api/types';

export interface Access {
	canWrite: boolean;
	/** Why writes are unavailable; empty when canWrite. Shown next to every disabled action. */
	reason: string;
	/** Org the session user is limited to, or null for admins (whole tenant). */
	scopeOrg: string | null;
}

/** Derives what the signed-in user may do in Knox. orgName is the display name of user.orgCode. */
export function accessOf(user: User | null, orgName: string): Access {
	if (!user) return { canWrite: false, reason: 'You are not signed in.', scopeOrg: null };
	if (user.role === 'admin') return { canWrite: true, reason: '', scopeOrg: null };
	if (!user.orgCode) {
		return {
			canWrite: false,
			reason: 'Your account has no organization assigned. Ask an administrator.',
			scopeOrg: null
		};
	}
	if (user.access !== 'write') {
		return { canWrite: false, reason: `Your access to ${orgName} is read-only`, scopeOrg: user.orgCode };
	}
	return { canWrite: true, reason: '', scopeOrg: user.orgCode };
}

export function displayName(u: { userName: string; firstName: string; lastName: string; userId: string }): string {
	return u.userName || `${u.firstName} ${u.lastName}`.trim() || u.userId;
}

/** Hidden owners (userId starting "_") have no user page, so they render as plain text. */
export const ownerLinkable = (userId: string) => !userId.startsWith('_');
