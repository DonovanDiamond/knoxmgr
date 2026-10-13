# Questions

Decision log. Append-only in spirit: a reversed decision gets a new entry, not an edit.
See [README.md](README.md) and [IMPLEMENTATION.md](IMPLEMENTATION.md).

## Which browsers are supported?

**Evergreen browsers only — a deliberate deviation from the house Firefox ESR 115 rule.**

knoxmgr is an admin tool used by IT staff on managed, current machines, not a kiosk or embedded
panel. Holding to ESR 115 would cost modern CSS and JS for no audience that needs it.

## Do we mirror Knox users and groups locally?

**No. Knox is the source of truth.**

A mirror needs sync, conflict handling and staleness rules, and would show data that Knox has
already changed. Reading live costs latency but cannot be wrong. The local database holds only app
accounts, refresh tokens, settings and the audit log.

## How is a non-admin user's access defined?

**One Knox org plus its sub-orgs, with access `read` or `write`.**

One org keeps the scope check a single prefix/ancestry test. Multiple orgs per account was
rejected as a larger rule surface; someone needing two orgs gets an admin or two accounts. Sub-orgs
are included because the org tree is how the tenant already delegates.

## Who sees user groups, and who changes them?

**Any org user sees all groups, but members are filtered to their scope. Only admins create or
delete groups.**

Group names are tenant-wide and a user needs them to add their own people. Revealing out-of-scope
members would leak users from other orgs, so those are hidden. A group is not owned by any org, so
no org user may create or delete one. Write users may add or remove in-scope members only.

## Which Knox groups are managed?

**USER groups only.**

Device groups and org management are out of scope; the product is about people and their
enrollment. Knox has no group-rename endpoint, so none is offered.

## Where do Knox credentials live?

**In `config.yaml`, which is git-ignored.**

One tenant per installation, so there is nothing to select at runtime and no need for a
credentials table or in-app secret editing. The file is the deployment's secret boundary.

## How are app sessions authenticated?

**Short-lived Ed25519-signed JWT access tokens, plus refresh tokens stored hashed in the DB and
delivered in an httpOnly cookie.**

Ed25519 gives small signatures and no shared secret to distribute; keys are generated on first
run. DB-backed refresh tokens can be revoked on logout or account disable, which a pure JWT cannot.
The httpOnly cookie keeps the long-lived token out of reach of page scripts. The access token is
kept in memory by the SPA.

## How does the first admin come to exist?

**From `bootstrap` in config.yaml, or `knoxmgr -create-admin`.**

Without an admin nobody can sign in to create one. Config suits unattended container deploys; the
CLI flag avoids leaving a password in a file.

## What happens when AD is down?

**Login returns 503 with code `ad_unreachable`; local accounts still work.**

A generic "bad credentials" would send users to reset passwords that are fine. Local accounts
remain so an admin can still get in during a directory outage.

## Is the refresh cookie Secure?

**Yes by default; `secureCookies: false` in config.yaml removes the flag.**

The cookie is a 30-day credential, so it should never cross plain HTTP. Intranet installs without
a TLS proxy would otherwise be unable to keep a session, so they set `secureCookies: false`
explicitly. This replaces the earlier old insecure-cookies start-up flag (see "Does `secureCookies`
replace the flag?" below). The cookie is also `HttpOnly`, `SameSite=Strict` and scoped to
`/api/auth`.

## What if an AD user has the same name as a local account?

**The local account wins; that name signs in locally only.**

Otherwise anyone able to create an AD account named `admin` could try to take over a local admin.
Local accounts are tried first and never fall through to AD, which also keeps them working when
AD is down. An AD login that collides with a local row is refused (`ErrSourceConflict`) rather
than merged.

## How are usernames compared?

**Case-insensitively: stored lower-case, lower-cased on every login.**

AD treats `Alice` and `alice` as one person; two rows would split their sessions and audit trail.
The `app_users` CHECK constraint enforces lower-case so no code path can bypass it.

## Are access tokens re-checked against the database?

**Superseded: yes, on every request (see "Is the account re-checked on every request?").**

Originally access tokens were verified locally and a disabled user kept access for up to
`jwt.accessTTL`. That window was removed in M2.

## Does knoxmgr need an AD service account?

**No. `go-ad-auth` binds as the signing-in user.**

The user's own credentials are used to authenticate and to read `displayName` and group
membership, so there is no stored directory password to protect or rotate. The config therefore
has no `ad.bindUser` or `ad.bindPassword`.

## Is the minimum Go version 1.26?

**Yes, accepted.** `go.mod` says `go 1.26.0`; building needs that toolchain or newer.

## Does `secureCookies` replace the flag?

**Yes.** The old insecure-cookies start-up flag is removed. `secureCookies` in config.yaml (default true)
controls the `Secure` flag, so the choice lives with the rest of the deployment config and there
is one place to look.

## What happens when a refresh token is replayed?

**All of that user's sessions are revoked.**

A refresh token is single-use. Seeing a revoked one again means it was copied or replayed, and we
cannot tell which holder is the thief, so every refresh token of the user is revoked and they sign
in again.

## How is login brute force limited?

**In-memory counter per client IP + username: 5 failures in 15 minutes gives 429.**

Cheap and needs no schema. Keying on both stops one IP spraying many names without locking a real
user out from everywhere. State is lost on restart and not shared between instances, which is
acceptable for one binary per tenant.

## Is the account re-checked on every request?

**Yes.** `Authenticate` re-loads the app account after verifying the token, and builds the
Principal from the database row. A disabled or deleted account, or a changed role or org, applies
on the next request. Cost: one indexed lookup per request.

## What does `ErrSourceConflict` map to?

**409 Conflict.** An AD login whose username matches a local row is refused (login returns 409,
"username belongs to an account from another source"); elsewhere it falls under "already exists".
A Knox application failure is a separate 502 `knox_error`.

## What is git-ignored?

**`/config.yaml` and `.claude/`**, in addition to keys, databases and build output. config.yaml
holds Knox credentials; `.claude/` is local agent state.

## Which HTTP verbs does the Knox client use?

**The ones in the spec.** The `select*` operations are `GET` with a query string; create, update,
purge, group and enrollment operations are `POST` with a form body. ID lists are joined with `;`,
and ids containing `;` are refused client-side.

## How is the Knox user list filtered by org?

**knoxmgr fetches all users (paged, bounded) and filters in the API.**

The spec's `selectUsers` has no org-code filter, so scope cannot be pushed to Knox. Filtering is
done by `auth.FilterByOrg` against the scope's org set. This costs bandwidth on large tenants; the
list is not cached.

## Which error codes does the mock Knox return?

**`1001` bad parameter, `1002` not found, `1003` conflict. These are invented.** The spec does not
list concrete resultCodes, and the real client treats any non-`0` code as failure, so the numbers
only need to be stable for tests.

## Are group lists really visible to everyone?

**Yes, including member counts.** Any signed-in app user sees every group and its `userCount`;
only the member list is filtered to scope. This is a small disclosure (how many members exist in
total) accepted because group names are tenant-wide and a user needs the group to add their own
people.

## What does a user get for a Knox user outside their scope?

**404 "user not found", the same as a user that does not exist.** A 403 would reveal that the id
exists in another org. This applies to get, update, purge, invite and group member add/remove
targets. Org codes the caller supplies (create, moving a user to another org) stay 403, since
nothing is revealed there. Knox transport failures (timeout, connection, token endpoint) are 502
`knox_unreachable`; Knox answering with an error stays 502 `knox_error`.

## What if the audit write fails after a mutation?

**It is logged, not fatal.** The Knox change has already happened and cannot be rolled back, so
failing the request would only mislead the caller into retrying.

## Config key naming

**Keys stay camelCase, including `secureCookies` and `knox.baseURL`.** `knox.baseURL` must be the
host only (for example `https://eu01.manage.samsungknox.com`); the client appends `/emm/oapi`, so
any path is rejected at startup.

## Which UI rendition was chosen?

**03 "Field coordinator"**, chosen by the user. It is built in `web/`; the other renditions stay
under `design/renditions/` for reference only.

## How is a user's device state derived?

**In the API, per request, from the Knox device list.** Supersedes the earlier `enrollment` field
(`enrolled`/`no_email`/`invited`/`not_invited`), which is removed. User JSON carries
`devices {enrolled, lastConnection, freshness}`. Freshness: 24h or less `fresh`, over 24h `stale`,
over 7 days `old`; 0 enrolled devices `none` (grey); an enrolled device that never connected is
`old`. The stalest enrolled device decides the colour (user's answer, 2026-10-12). Invitation time
is still shown, as "last invited" on the user screen, from the `invites` table (see below).

## Why an invites table?

**The audit target is one comma-joined string, so "latest invite per user" had to be a LIKE scan of
the log, which cannot use an index.** `invites` has one row per user per invite, is indexed on
`(user_id, invited_at)`, matches ids exactly and is append-only. The audit log stays the record of
who did what; `invites` only answers "when was this user last invited". Only ids Knox actually
invited get a row (not `notExistUserId` or `userIdWithoutEmail`), while the audit target still
lists every requested id. Migration `0002` backfills from existing `knox.user.invite` audit rows,
splitting the target in Go because SQLite and Postgres share no string-split function.

## What counts as enrolled?

**A device with Knox status `A`, `B`, `BS`, `BA` or `BL`.** Provisioned (`P`) and deactivated (`I`)
devices are not enrolled; blocked ones still are. Supersedes the earlier `deviceCount.enrolled` rule.

## Which devices can a coordinator see?

**Only devices whose user is in their org subtree; anything else is 404.** Unassigned devices are
admin-only (user's answer, 2026-10-12). Knox's device list has no orgCode, so scope comes from the owner.

## Is the display name derived?

**Yes, but editable.** It starts as "first last" and follows the name fields until the user edits it
by hand, then stops following (user's answer, 2026-10-12).

## Open points

- `selectDevicesByUser` is typed in the spec as a single device although it documents a list; the
  client assumes `{devicelist:[...]}` like `selectDeviceList`. Confirm against a real tenant.
- Knox has no per-user group endpoint, so the user screen finds a user's groups by listing every
  group's members.

## How are fonts loaded?

**Figtree is self-hosted through `@fontsource/figtree`, bundled into `web/build`.** There is no
Google Fonts request at runtime, so the UI works offline and leaks nothing to a third party.

## What does a non-200 Knox HTTP answer map to?

**502 `knox_unreachable`**, the same as a transport failure: Knox did not give a usable answer. Only
a Knox envelope with `resultCode != 0` is `knox_error`.

## Can the edit form clear a field?

**No.** Knox treats an empty value as "unchanged" on update, so the form cannot blank a field that
already has a value.

## How does the CSP allow the inline scripts?

**`script-src 'self'` plus sha256 hashes computed at start-up from the embedded `index.html`.** The
theme pre-paint script stays inline, and SvelteKit's bootstrap script is also inline and changes
with each build, so a static policy file alone would not satisfy CSP.

## Who sees Knox's error text?

**Only Knox HTTP 4xx text is admin-only** (others get "Knox rejected the request"; the detail is
logged). `resultCode` business errors are still shown to every user.

## How is the client IP taken behind a proxy?

**`EnableIPValidation` is set whenever `trustedProxies` is non-empty**, so fiber returns the
rightmost `X-Forwarded-For` hop that is not a trusted proxy instead of the raw, spoofable header.

## What if the Origin header is absent on refresh and logout?

**It passes.** Non-browser clients send none, and `SameSite=Strict` already covers the cookie in
browsers. A present Origin must match the request host.

## Does a successful login clear the per-IP counter?

**No.** Only the IP+username counter is cleared, so one valid login cannot reset the budget of an IP
that is guessing other accounts.

## What does the limiter drop when it is full?

**The oldest 10% by window start**, after expired entries, once 10000 keys are held by live entries.
Dropping everything would let an attacker reset all counters by filling the table.

## Why a dummy bcrypt on the AD path?

**So a non-local username costs one bcrypt whether or not AD is configured**, keeping the timing of
local-versus-unknown names indistinguishable.

## How is HSTS set?

**`max-age=31536000` without `includeSubDomains`**, and only when secure cookies are on. Other
subdomains on the host are not ours to commit to HTTPS.

## What identifies an unknown username in the failed-login log?

**The first 12 hex characters of its SHA-256.** A real account is logged by name; a mistyped one may
be a password, so it is logged only as a hash that still lets repeats be correlated.

## Where do Knox credentials live now? (reverses "Where do Knox credentials live?")

**In the database, edited on admin settings screens; secrets encrypted at rest.**

A colleague should be able to run the binary and connect their own tenant without editing files, and
an admin should be able to change Knox or AD settings, with a Test button, without a restart.
`config.yaml` is now optional and holds only what is needed before the database opens (listen
address, timezone, cookies, proxies, database, token lifetimes). Secrets are sealed with AES-256-GCM
under an auto-generated `secret.key` in the data directory and are never sent back to the browser;
saving with an empty secret field keeps the stored one. Old `knox`, `ad`, `installationName`,
`bootstrap` and `jwt.*KeyPath` keys in config.yaml are rejected with a message rather than ignored.

## How does the first admin come to exist now? (reverses "How does the first admin come to exist?")

**On a browser setup page guarded by a one-time code in the log.** `-create-admin` stays for recovery.

Whoever can read the process log owns the install, which keeps a freshly exposed port from being
claimed by a stranger. See the next entries for the details.

## Is the setup code stored?

**No, it is held in memory only.** A new code is generated and logged on every start until an admin
exists. Nothing on disk can leak it or go stale, and a restart is the way to get a fresh one. It is
cleared when the admin is created. Wrong guesses are rate limited per IP.

## Is `/api/health` behind the setup gate?

**No, it is exempt.** Until the first admin exists every other `/api` route except `/api/setup/*`
answers 409 `setup_required`. Health stays open because container probes need it and because the SPA
reads `setupRequired` and `knoxConfigured` from it to decide between the setup page, the login page and
the "Knox is not connected" notice. It reveals only the installation name and those two flags.

## Is the Knox token URL a setting?

**No, it is always derived** as `baseURL + /emm/oauth/token`. Knox hosts all use that path, so a
separate field only offered a way to get it wrong. The settings screen asks for a region (preset base
URL) or a custom host-only base URL.

## Why are AD bind user and password stored if they are unused?

**They are stored (the password encrypted) but not used.** `go-ad-auth` binds as the signing-in user,
which is why the earlier decision (no AD service account) still holds for authentication. The fields
are kept in the settings so a later directory lookup that needs a service account does not need a
schema change or another migration of secrets.

## What if a stored secret cannot be decrypted?

**Knox (or AD) is switched off and an error is logged; the app still boots.** A lost or replaced
`secret.key` would otherwise lock everyone out, including the admin who could fix it. The admin signs
in locally, re-enters the secret on the settings screen and saves. Knox routes answer 503
`knox_not_configured` meanwhile.

## Do we mirror Knox users and groups locally now? (reverses "Do we mirror Knox users and groups locally?")

**Yes. Knox stays the source of truth, but knoxmgr keeps a synced local mirror.** Decided 2026-10-12;
see decision 2026-10-12 in CLAUDE.md. Reads come from the mirror, writes go to Knox first.

## AD bind user/password: hidden in the UI

**Not used: go-ad-auth binds as the signing-in user; fields hidden, columns kept.**

## Device detail, apps and location (M8): defaults chosen

**Pending user confirmation.** Chosen defaults, recorded 2026-10-12:
- Detail, apps and location are fetched live from Knox when the screen opens, cached per device and kind (location per 1/7/30-day window) in `knox_device_cache`, and served with an "as of" note only when Knox is unreachable (`knox_unreachable`), not on Knox business errors. They are not part of the bulk sync; cache rows are pruned when a sync drops the device.
- Location is for admins and write users only. Read users get 404, the same as for an out-of-scope device, chosen over 403 so the screen does not reveal that a location exists. Every location response is audited `device.location.view` `{days, cached}`.
- No embedded map (CSP and privacy); coordinates are shown with an OpenStreetMap link-out.
- Knox has no agent or client version field, so the "Agent profile" and "Client profile" names are shown instead.
- History uses Knox's `period` (whole days: 1, 7 or 30).
- Location calls send `deviceId` only; the spec also lists `mobileId`. Untested against a real tenant.
- The device list badge reuses the per-device freshness, so an enrolled device that never connected is red ("old"), not grey; grey is only for non-enrolled devices.
