import { ApiError } from './api/client';
import { listOrgs } from './api/knox';
import type { Organization } from './api/types';

export interface OrgNode {
	org: Organization;
	depth: number;
}

class Orgs {
	list = $state<Organization[]>([]);
	loaded = $state(false);
	private pending: Promise<void> | null = null;

	/** Loads once; concurrent callers share the request. A failure is retried on the next call. */
	load(): Promise<void> {
		this.pending ??= listOrgs()
			.then((o) => {
				this.list = o;
				this.loaded = true;
			})
			// Without a Knox connection there are no orgs; pages show their own empty state.
			.catch((e) => {
				if (!(e instanceof ApiError && e.code === 'knox_not_configured')) throw e;
			})
			.finally(() => {
				if (!this.loaded) this.pending = null;
			});
		return this.pending;
	}

	/** Forgets the cache, e.g. after the Knox connection changed. */
	reset() {
		this.list = [];
		this.loaded = false;
		this.pending = null;
	}

	name(code: string | null | undefined): string {
		return this.list.find((o) => o.orgCode === code)?.orgName ?? code ?? '';
	}

	/** Depth-first tree of root and everything below it; root null means the whole tenant. */
	tree(root: string | null = null): OrgNode[] {
		const kids = new Map<string, Organization[]>();
		const known = new Set(this.list.map((o) => o.orgCode));
		const tops: Organization[] = [];
		for (const o of this.list) {
			if (o.parentCode && known.has(o.parentCode)) {
				kids.set(o.parentCode, [...(kids.get(o.parentCode) ?? []), o]);
			} else {
				tops.push(o);
			}
		}
		const out: OrgNode[] = [];
		const walk = (o: Organization, depth: number) => {
			out.push({ org: o, depth });
			for (const k of kids.get(o.orgCode) ?? []) walk(k, depth + 1);
		};
		if (root === null) tops.forEach((o) => walk(o, 0));
		else {
			const r = this.list.find((o) => o.orgCode === root);
			if (r) walk(r, 0);
		}
		return out;
	}

	/** Codes of root and all its sub-organizations. */
	scope(root: string | null): Set<string> {
		return new Set(this.tree(root).map((n) => n.org.orgCode));
	}
}

export const orgs = new Orgs();
