import type { Freshness } from '#lib/api/devices';

export type Role = 'admin' | 'user';

export interface User {
	id: number;
	username: string;
	displayName: string;
	role: Role;
	orgCode: string | null;
	access: 'read' | 'write' | null;
	authSource: 'local' | 'ad';
}

export interface AuthResponse {
	accessToken: string;
	expiresAt: string;
	user: User;
}

export interface Health {
	status: 'ok';
	name: string;
	version: string;
	knoxConfigured: boolean;
	setupRequired: boolean;
	secureCookies: boolean;
}

export interface ApiErrorBody {
	error: string;
	code?: string;
}

/** A Knox Manage user as returned by /api/knox/users. */
export interface KnoxUser {
	userId: string;
	userName: string;
	email: string;
	firstName: string;
	lastName: string;
	mobilePhone: string;
	contact: string;
	empNo: string;
	orgCode: string;
	orgName: string;
	enabled: string;
	userType: string;
	devices: { enrolled: number; lastConnection?: string; freshness: Freshness };
	invitedAt?: string;
}

/** Editable Knox user fields; empty means unset, and the server ignores empty fields on update. */
export interface KnoxUserFields {
	userName?: string;
	email?: string;
	firstName?: string;
	lastName?: string;
	mobilePhone?: string;
	contact?: string;
	empNo?: string;
	orgCode?: string;
}

export interface CreateKnoxUser extends KnoxUserFields {
	userId: string;
	password?: string;
	requestEnrollment?: boolean;
}

export interface Organization {
	orgCode: string;
	orgName: string;
	parentCode: string;
}

export interface OrgDetail {
	org: Organization;
	members: KnoxUser[];
}

export interface Group {
	groupId: string;
	groupName: string;
	groupType: string;
	userCount: number;
	/** Assigned in Knox, as of the last sync. */
	assignAppCount: number;
	assignProfileCount: number;
}

/** Users Knox could not email; everyone else in the request was invited. */
export interface EnrollmentResult {
	notExistUserId: string[] | null;
	userIdWithoutEmail: string[] | null;
}

/** An app account (admin API). */
export interface AppUser extends User {
	disabled: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface CreateAppUser {
	username: string;
	displayName?: string;
	password: string;
	role: Role;
	orgCode?: string;
	access?: string;
}

export interface UpdateAppUser {
	displayName?: string;
	role?: Role;
	orgCode?: string;
	access?: string;
	disabled?: boolean;
}

export interface AuditEntry {
	id: number;
	at: string;
	actorUserId: number | null;
	actorUsername: string;
	action: string;
	target: string;
	previousValue: string | null;
	newValue: string | null;
}

export interface KnoxRegion {
	id: string;
	label: string;
	baseURL: string;
}

export interface KnoxSettings {
	/** A region id, or "custom" to use baseURL. */
	region: string;
	baseURL: string;
	clientId: string;
	tenantId: string;
	secretSet: boolean;
	allowInsecureHTTP: boolean;
	configured: boolean;
	regions: KnoxRegion[];
}

export interface KnoxSettingsInput {
	region: string;
	baseURL?: string;
	clientId: string;
	tenantId: string;
	/** Omit to keep the stored secret. */
	clientSecret?: string;
	allowInsecureHTTP: boolean;
}

export type KnoxTestResult = { ok: true; message: string } | { ok: false; stage: 'token' | 'api'; message: string };

export interface AdMapping {
	group: string;
	role: Role;
	orgCode: string;
	access: 'read' | 'write';
}

export interface AdSettings {
	enabled: boolean;
	url: string;
	baseDN: string;
	upnSuffix: string;
	bindUser: string;
	bindPasswordSet: boolean;
	mappings: AdMapping[];
}

export interface AdSettingsInput extends Omit<AdSettings, 'bindPasswordSet'> {
	/** Omit to keep the stored password. */
	bindPassword?: string;
}

export type AdFailureCode = 'invalid_credentials' | 'no_mapped_group' | 'ad_unreachable' | 'ad_disabled';

export type AdTestResult =
	| { ok: true; displayName: string; groups: string[]; role: Role; orgCode: string; access: 'read' | 'write' }
	| { ok: false; code: AdFailureCode; message: string };

export interface GeneralSettings {
	installationName: string;
	syncIntervalMinutes: number;
	checkUpdates: boolean;
	showMap: boolean;
	appInventoryHours: number;
	appReportDaily: boolean;
}

export interface UpdateStatus {
	enabled: boolean;
	current: string;
	latest?: string;
	url?: string;
	updateAvailable: boolean;
	checkedAt?: string;
	error?: string;
}
