import { api } from './client';

const seg = encodeURIComponent;

export type Freshness = 'none' | 'fresh' | 'stale' | 'old' | 'never' | 'not_enrolled';

export interface Device {
	deviceId: string;
	modelName: string;
	deviceName?: string;
	os: string;
	osVersion?: string;
	status: string;
	enrolled: boolean;
	lastConnection?: string;
	freshness: Freshness;
	userId?: string;
	userName?: string;
	orgCode?: string;
}

export const listDevices = (q = '') =>
	api<{ devices: Device[]; total: number }>(`/api/knox/devices${q ? `?q=${seg(q)}` : ''}`);
export const getDevice = (id: string) => api<Device>(`/api/knox/devices/${seg(id)}`);
export const listUserDevices = (userId: string) =>
	api<{ devices: Device[]; total: number }>(`/api/knox/users/${seg(userId)}/devices`);

export interface DeviceInfo {
	model?: string;
	modelName?: string;
	os?: string;
	osVersion?: string;
	imei?: string;
	serialNumber?: string;
	carrier?: string;
	phoneNumber?: string;
	batteryLevel?: string | number;
	rooted?: boolean;
	agentVersion?: string;
	clientVersion?: string;
	createdAt?: string;
	lastConnection?: string;
}
export interface DeviceDetail {
	info: DeviceInfo;
	profiles: string[];
	fetchedAt: string;
	cached: boolean;
	canViewLocation: boolean;
}
export interface DeviceApp {
	name: string;
	packageName: string;
	version: string;
}
export interface GeoPoint {
	time: string;
	latitude: number;
	longitude: number;
	altitude?: number;
}
export interface DeviceLocation {
	current: GeoPoint | null;
	history: GeoPoint[];
	days: number;
	fetchedAt: string;
	cached: boolean;
	showMap: boolean;
}

export const getDeviceDetail = (id: string) => api<DeviceDetail>(`/api/knox/devices/${seg(id)}/detail`);
export const getDeviceApps = (id: string) =>
	api<{ apps: DeviceApp[]; fetchedAt: string; cached: boolean }>(`/api/knox/devices/${seg(id)}/apps`);
export const getDeviceLocation = (id: string, days: 1 | 7 | 30) =>
	api<DeviceLocation>(`/api/knox/devices/${seg(id)}/location?days=${days}`);
