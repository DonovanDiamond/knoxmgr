# Implementation

Related documents: [README.md](README.md) (what it is) · [QUESTIONS.md](QUESTIONS.md) (why) ·
[ARCHITECTURE.md](ARCHITECTURE.md) (module map, flows) ·
[CODE-BLOCKS-REFERENCE.md](CODE-BLOCKS-REFERENCE.md) (block to file index).

Go module `github.com/DonovanDiamond/knoxmgr`. Stack: Go, gofiber v3, zerolog, sqlx, pure-Go
SQLite (optional Postgres via pgx), embedded migrations, bcrypt, Ed25519 JWT
(golang-jwt/v5), `go-ad-auth` for AD; SvelteKit 3 (Svelte 5 runes, TypeScript, adapter-static)
embedded in the binary; Tailwind v4 and daisyUI. `CGO_ENABLED=0`.

## Layout

```
cmd/knoxmgr/   flags and data dir only; calls server.Run
cmd/knoxmgr-desktop/  portable Windows app (windows build tag): server on loopback in a WebView2 window
src/server/    shared start: load config, open db, migrate, build services, serve (Options, Run, -create-admin)
src/config/    optional config.yaml loading, defaults, validation, data dir, Secret type
src/secretbox/ AES-256-GCM encryption of stored secrets; key file in the data dir
src/settings/  Manager: validates and saves Knox / AD / general settings, rebuilds the live Knox client and AD directory
src/db/        connection, dialect shim, embedded migrations, repositories
src/auth/      local + AD login, password hashing, JWT issue/verify, keys, refresh rotation, role helpers, org Scope, login rate limiter, first-run SetupCode
src/knox/      typed Knox Manage client (OAuth token reuse, users, groups, orgs)
src/sync/      Scheduler: background Knox-to-mirror sync (interval, triggers, single-flight)
src/knoxmock/  in-memory fake Knox server (development and tests)
src/api/       fiber routes, middleware, JSON error shape, audit recording, embedded-UI serving
cmd/knoxmock/  runs the fake Knox server
web/           SvelteKit app; web/embed.go embeds web/build into the binary
docs/knox-api/ Knox OpenAPI spec (km-api.yml, v25.07)
```

`example/` holds a Docker Compose demo stack; see Packaging.

Role checks (`RequireAdmin`, `RequireWrite`) live in `src/auth/principal.go`. The org sub-tree
scope check is `auth.Scope` in `src/auth/scope.go`; every Knox route reaches it through there.
Org codes are resolved from the mirror (`Mirror.SubOrgCodes`, a recursive query on `knox_orgs`), never taken from the client.

## config.yaml

The file is **optional**. Without it (and without an explicit `-config`) the defaults below apply.
If `-config` is given the file must exist. Unknown keys are rejected. It holds only what is needed
before the database is open; everything else is in the database (see Settings).

```yaml
listenAddr: ":8080"                   # default; host:port
timezone: "UTC"                       # default; IANA name
secureCookies: true                   # default true; false only for plain-HTTP installs
trustedProxies: []                    # IPs or CIDRs of reverse proxies whose X-Forwarded-For is believed
database:
  driver: sqlite                      # default sqlite | postgres
  dsn: "<dataDir>/knoxmgr.db"         # default under the data dir; treated as a secret (never printed)
jwt:
  accessTTL: 15m                      # default 15m
  refreshTTL: 720h                    # default 720h (30 days)
```

Validation at load: `listenAddr` is host:port; `timezone` known; `database.driver` is `sqlite` or
`postgres` and `dsn` set; TTLs positive; every `trustedProxies` entry is a CIDR or an IP. Every
problem is reported, one per line. `Secret` fields print as `[redacted]`.

Keys that used to live here are **rejected with an explanatory error** rather than ignored:
`installationName`, `knox` and `ad` ("moved to admin settings"), `bootstrap` ("the first admin is
created on the setup page") and `jwt.privateKeyPath` / `jwt.publicKeyPath` ("keys live in the data
dir"). All offending keys are listed together.

### Data directory

Resolved from `-data`, else env `KNOXMGR_DATA`, else `./data`; created with mode 0700. It holds
`knoxmgr.db` (SQLite default), `jwt-private.pem` / `jwt-public.pem` (Ed25519 pair, generated when
absent) and `secret.key` (the secrets key). Back it up as one unit: the database is unreadable in
part without `secret.key`. In the Docker image the working directory is `/var/lib/knoxmgr`, so the
default lands in `/var/lib/knoxmgr/data`.

## Command line

| Flag | Meaning |
|---|---|
| `-config <path>` | config file, default `config.yaml`; a missing default file is fine, a missing explicit one is an error |
| `-data <dir>` | data directory; default env `KNOXMGR_DATA`, else `./data` |
| `-listen <host:port>` | listen address; default env `KNOXMGR_LISTEN`, else `listenAddr` from the config; must be host:port |
| `-create-admin <username>` | recovery tool: create a local admin, then exit (the first admin normally comes from the setup page). Password from env `KNOXMGR_ADMIN_PASSWORD`, else prompted without echo (or one line from piped stdin). 8-72 bytes. |

Plain-HTTP installs (no TLS proxy) set `secureCookies: false` in `config.yaml`; otherwise the
browser will not send the `Secure` refresh cookie back.

Behind a proxy, `trustedProxies` makes fiber believe `X-Forwarded-For` from those peers and walk it
right to left (`EnableIPValidation`), so the client IP is the rightmost untrusted hop. Empty means
the socket peer is used and the header is ignored.

### Mock Knox (`cmd/knoxmock`)

A fake Knox Manage for development and tests, seeded with orgs `ROOT` > `SALES` > `SALES_EMEA` and
`ROOT` > `SUPPORT`, six users and two groups. State is in memory.

```
go run ./cmd/knoxmock -client-id demo -client-secret demo      # listens on 127.0.0.1:9090
```

Flags `-addr`, `-client-id`, `-client-secret`, or env `KNOXMOCK_ADDR` (default `127.0.0.1:9090`),
`KNOXMOCK_CLIENT_ID`, `KNOXMOCK_CLIENT_SECRET`; id and secret are required. Point knoxmgr at it on
the Knox settings screen (the setup page's last step, or `/admin/settings/knox`): region "Custom
URL", base URL `http://127.0.0.1:9090`, the client ID and secret as given, any tenant ID, and "allow
insecure HTTP" ticked (the mock is plain HTTP). Error codes `1001` (bad parameter),
`1002` (not found), `1003` (conflict) are invented; the spec lists none. It also serves
`selectDeviceAppList`, `selectDeviceLocation` and `selectDeviceLocationHistory` with generated apps and a
30-day location trail per device, so the device screen works end to end.

## Settings

Knox connection, Active Directory and general settings are stored in the database and edited by
admins in the web UI, not in `config.yaml`. `src/settings.Manager` validates input, encrypts
secrets, saves with an audit row, and applies the change live (no restart): it swaps in a new
`knox.Client` and a new AD directory (`auth.Service.SetDirectory`).

- **Knox**: `region` (a preset id `ap01`..`ap05`, `eu01`..`eu08`, `us01`..`us03`, or `custom`),
  `baseURL`, `clientId`, `tenantId`, `clientSecret`, `allowInsecureHTTP`. A preset region fixes
  `baseURL` and forces `allowInsecureHTTP` off. A custom `baseURL` is an http(s) URL with no path,
  and plain `http` needs `allowInsecureHTTP`. The token URL is always `baseURL + /emm/oauth/token`
  and is not a setting. Knox counts as configured when base URL, client ID, tenant ID and a secret
  are all present; until then every `/api/knox/*` route answers 503 `knox_not_configured`.
- **Active Directory**: `enabled`, `url` (`ldap://` or `ldaps://`), `baseDN`, `upnSuffix`,
  `bindUser`, `bindPassword`, and group `mappings` (group, role, orgCode, access; same rules as
  before: admin mappings carry no org/access, user mappings need both). Enabling needs a base DN and
  at least one mapping. `bindUser` and `bindPassword` are stored but unused: `go-ad-auth` binds as
  the signing-in user. A `ldap://` URL logs a warning when applied.
- **General**: `installationName` and `syncIntervalMinutes` (1-1440, default 15). The name is
  returned by `/api/health`. Saving it makes the sync loop re-read the interval at once.
  `showMap` (default on; omitted on PUT keeps the stored value) controls the device location map.
  The toggle carries a privacy note: the viewer's browser loads tiles from OpenStreetMap.

### Secrets encryption

The Knox client secret and AD bind password are sealed with AES-256-GCM (`src/secretbox`), a random
nonce per value, stored as base64(nonce||ciphertext) in the `*_cipher` columns. The 32-byte key is in
`<dataDir>/secret.key`, created once (mode 0600, random) and never rotated by knoxmgr. Secrets are
never returned by the API: views carry `secretSet` / `bindPasswordSet` booleans, and a save with an
empty secret field keeps the stored one. If a stored secret cannot be decrypted (lost or changed
key), start-up does not fail: the error is logged, Knox (or AD) stays off, and the admin re-enters
the secret.

### First-run setup

When start-up finds no admin account, it generates a one-time setup code (12 symbols from a 32
symbol alphabet without `0 1 I O`, shown as `XXXX-XXXX-XXXX`) and logs it as a warning with the
message `first-run setup code` and field `setupCode`. The code is held in memory only: a restart
before an admin exists produces a new code, and it is cleared once the admin is created.

While the code is pending, the setup gate answers every `/api/*` route except `/api/health` and
`/api/setup/*` with 409 `setup_required`. The SPA reads `setupRequired` from `/api/health` and sends
the browser to `/setup`: (1) enter the code, (2) create the admin (the admin is signed in), (3)
Knox settings with Test and Save, or "Skip for now", (4) done. Wrong codes are rate limited (own
limiter, 5 per 15 minutes per IP) and logged without the code. If an admin already exists the setup
routes answer 409 `setup_done`. `-create-admin` remains for recovery.

## Schema

Migrations are embedded SQL files in `src/db/migrations/`, numbered, **append-only**: a shipped
migration is never edited, a new one is added. Each runs in a transaction. `{{pk}}` in the SQL is
the one engine-specific token (`INTEGER PRIMARY KEY AUTOINCREMENT` on SQLite, `BIGSERIAL PRIMARY
KEY` on Postgres). Timestamps are UTC ISO-8601 text (`YYYY-MM-DDTHH:MM:SSZ`). SQLite connections
get foreign keys on, WAL and a 5s busy timeout.

- `schema_migrations` — `version` (pk), `name`, `applied_at`. Created by the migrator.
- `app_users` — `id`, `username` (unique, non-empty, lower-case enforced by CHECK),
  `display_name`, `password_hash` (null for AD), `role` (`admin`|`user`), `org_code`, `access`
  (`read`|`write`), `auth_source` (`local`|`ad`), `disabled`, `created_at`, `updated_at`.
  CHECKs: a `user` must have `org_code` and `access`; a `local` account must have `password_hash`.
- `refresh_tokens` — `id`, `user_id` (fk `app_users`), `token_hash` (unique; SHA-256 hex, the
  token itself is never stored), `expires_at`, `revoked_at`, `created_at`.
- `audit_log` — `id`, `at`, `actor_user_id` (fk, nullable), `actor_username`, `action`,
  `target`, `previous_value`, `new_value`.
- `invites` — `id`, `user_id` (Knox user id), `invited_at` (UTC), `invited_by` (fk `app_users`,
  nullable; nulled when that account is deleted). Index `invites_user_id_invited_at` on
  (`user_id`, `invited_at`). One row per successfully invited id, written with the `knox.user.invite`
  audit row in one transaction (`Audit.InsertInvites`). Migration `0002` backfills it from existing
  audit rows through a per-version Go hook in `migrate.go` (same transaction), since SQLite and
  Postgres have no common string-split.
- `settings` — `key` (pk), `value`, `updated_at`. `Settings.Set` writes an `audit_log` row in the
  same transaction. Not used by the admin settings screens.
- `knox_settings` (migration `0003`) — one row (`id` = 1, seeded): `region`, `base_url`, `client_id`,
  `tenant_id`, `client_secret_cipher`, `allow_insecure_http`, `updated_at`.
- `ad_settings` — one row (`id` = 1): `enabled`, `url`, `base_dn`, `upn_suffix`, `bind_user`,
  `bind_password_cipher`, `updated_at`.
- `ad_group_mappings` — `id`, `position` (order), `group_dn`, `role`, `org_code`, `access`. Replaced
  as a whole on each AD save.
- `general_settings` — one row (`id` = 1): `installation_name` (default `knoxmgr`),
  `sync_interval_minutes` (default 15, CHECK 1-1440), `show_map` (0/1, default 1, migration
  `0007`), `updated_at`.
- Knox mirror (migration `0004`), keyed by the Knox ids, every row with `synced_at`: `knox_orgs`
  (`org_code`, `org_name`, `parent_code`), `knox_users` (profile fields, `org_code`, `enabled`,
  `device_count`), `knox_groups` (USER groups; `user_count` is recomputed from memberships),
  `knox_group_members` (`group_id`, `user_id`), `knox_devices` (`user_id`, `org_code` of the owner).
- `knox_device_cache` (migration `0005`) — `device_id`, `kind` (`detail`, `apps`, `location:1`/`:7`/`:30`),
  `payload` (JSON), `fetched_at`; primary key (`device_id`, `kind`). The last live Knox answer, served only
  while Knox is unreachable. Not part of the bulk sync; `ReplaceAll` deletes rows of devices the sync dropped.
- `sync_runs` — `id`, `trigger` (`startup`, `scheduled`, `knox_saved`, `manual`), `started_at`,
  `finished_at` (null while running), `status` (`running`|`ok`|`failed`), `error`, and counts
  `orgs`, `users`, `groups`, `memberships`, `devices` (zero on failure).

Audit actions: `setup.create_admin`, `user.create_admin_cli`, `settings.set`,
`settings.knox`, `settings.ad`, `settings.general` (previous/new value; secrets show only whether they changed);
`app_user.create`, `app_user.update`, `app_user.delete`;
`knox.user.create`, `knox.user.update`, `knox.user.purge`, `knox.user.invite`;
`knox.group.create`, `knox.group.delete`, `knox.group.add_members`, `knox.group.remove_members`;
`admin.sync.start`.
Rows hold actor, target and previous/new value (JSON; never passwords). The API writes the row
after the Knox/DB change succeeded; a failed audit write is logged, not returned. Logins are not
audited.

All SQL is parameterized.

## Knox mirror and sync

Reads (`/api/knox/*` GET) come from the mirror tables; scope is applied in SQL (`org_code IN` the
caller's org and descendants; admins unrestricted). Devices are scoped by the owner's org
(`knox_devices.org_code`, filled from the owner user at sync time); unassigned devices are admin-only.

- **Triggers** (`src/sync.Scheduler`): once at start-up if Knox is configured (`startup`), every
  `syncIntervalMinutes` (`scheduled`), after the Knox settings are saved (`knox_saved`), and
  `POST /api/admin/sync` (`manual`). Saving the general settings re-reads the interval and restarts
  the countdown. Knox not configured: the trigger is skipped.
- **Single-flight**: one run at a time; a trigger during a run returns that run's id
  (`alreadyRunning`). Runs use the application context, not the request's.
- **Fetch**: orgs, all users, USER groups (other group types skipped), each group's members
  (members unknown to the fetched users are dropped), all devices; paged 1000 at a time, at most 20
  pages per list, beyond that the run fails rather than sync a partial list.
- **Replace-set**: the whole snapshot replaces the mirror in one transaction (`Mirror.ReplaceAll`);
  group counts are recomputed.
- **Failure keeps the mirror**: any fetch or write error rolls back, the mirror stays as it was, and
  the `sync_runs` row ends `failed` with the error text.
- **Write-through**: after Knox accepts a user create/update/purge, group create/delete or
  membership change, the mirror row is patched; if that patch fails it is logged and the next sync
  repairs it. The audit row is written as before.

## Authentication

- Login: username is trimmed and lower-cased. If a local account of that name exists it is
  checked with bcrypt and never falls through to AD. Otherwise, if AD is enabled, `go-ad-auth`
  binds as the user and reports `displayName` and which mapped groups the user is in; the account
  is upserted from the mapping (role/org/access follow AD on every login; `disabled` is kept).
  Otherwise a dummy bcrypt comparison runs and the login fails as invalid credentials. The AD path
  also runs one dummy bcrypt first, so non-local attempts cost the same either way.
- Login rate limit: failed logins (invalid credentials only) are counted in memory per client IP
  plus normalised username (lower-case, `DOMAIN\` and `@suffix` removed): 5 failures within 15
  minutes, or 30 per IP alone, gives 429 until the window ends. A successful login clears the
  IP+username count, not the per-IP one. When 10000 keys are held, expired ones are dropped, then
  the oldest 10%. State is lost on restart.
- A failed login is logged with the username only if the account exists, otherwise 12 hex of its
  SHA-256 (`userHash`).
- Access token: Ed25519 (EdDSA) JWT, issuer `knoxmgr`, claims `uid`, `username`, `role`,
  `orgCode`, `access`. Sent as `Authorization: Bearer`. `Service.Verify` checks the token only, but
  the `Authenticate` middleware then re-loads the account on every request: a deleted account is
  401, a disabled one 403, and the role/org/access used are those in the database, not the token.
- Refresh token: 32 random bytes, base64url; only the SHA-256 hash is stored. Refresh rotates it
  (old one revoked, new one issued), reloads the user, and revokes all of a disabled user's
  tokens. Presenting an already-revoked token (replay) revokes every session of that user.
  Two concurrent uses of one token: only one wins.
- Refresh cookie: name `knoxmgr_refresh`, `Path=/api/auth`, `HttpOnly`, `SameSite=Strict`,
  `Secure` unless `secureCookies: false`, `Max-Age` = `refreshTTL`. Cleared (`Max-Age=-1`) on logout
  and on a failed refresh.

## API

Errors are JSON `{"error": "...", "code": "..."}`; `code` is set for `ad_unreachable`, `knox_error`, `knox_unreachable`, `knox_not_configured`, `setup_required`, `setup_done` and `invalid_setup_code`.
Request bodies are limited to 1 MiB; login username max 256 bytes, password max 1024.
Responses carry `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: same-origin`, `Permissions-Policy: camera=(), microphone=(), geolocation=()` and,
when secure cookies are on, `Strict-Transport-Security: max-age=31536000`. `/api` responses are
`Cache-Control: no-store`. HTML pages carry a `Content-Security-Policy`: `default-src 'self'`,
`script-src 'self'` plus a sha256 hash of each inline script in `index.html` (computed at start-up),
`style-src 'self' 'unsafe-inline'`, `img-src 'self' data: https://tile.openstreetmap.org` (map tiles), `frame-ancestors 'none'`.
`POST /api/auth/refresh` and `/logout` return 403 when an `Origin` header names another host; no
`Origin` passes. A Knox HTTP 4xx answer shows its text to admins only; others get "Knox rejected
the request". Requests are logged (method, path, status, user, ip; never query
string or body).

| Route | Notes |
|---|---|
| `GET /api/health` | unauthenticated, exempt from the setup gate; `{status, name, version, knoxConfigured, setupRequired, secureCookies}` |
| `POST /api/setup/verify` | `{code}`; 204 if right, 401 `invalid_setup_code`, 429 when limited, 409 `setup_done` |
| `POST /api/setup/admin` | `{code, username, password}`; creates the first admin, clears the code, returns the login body and cookie; 409 `setup_done` |
| `POST /api/auth/login` | JSON `{username, password}`; returns `{accessToken, expiresAt, user}` and sets the refresh cookie |
| `POST /api/auth/refresh` | reads the cookie (401 if absent), rotates it, returns the same body as login |
| `POST /api/auth/logout` | revokes the refresh token if present, clears the cookie, 204 |
| `GET /api/auth/me` | requires Bearer; current user from the DB (401 unknown user, 403 disabled) |
| `GET /api/knox/orgs` | Bearer; org tree entries from the mirror filtered to the caller's scope; `{orgs}` |
| `GET /api/knox/users` | Bearer; optional `?q=` (user id contains, case-insensitive); read from the mirror, scoped in SQL by org; `{users, total}`; each user also carries `devices {enrolled, lastConnection, freshness}` (`freshness` is `none`, `fresh`, `stale` or `old`: 24h and 7d thresholds, 0 enrolled `none`, enrolled never connected `old`) and `invitedAt` (RFC 3339, only when invited); enrolled means Knox status `A`, `B`, `BS`, `BA` or `BL` |
| `POST /api/knox/users` | write; create a Knox user (org must be in scope) |
| `POST /api/knox/users/invite` | write; `{userIds}` (1-100, in scope), sends enrollment invitations |
| `GET /api/knox/users/:id` | Bearer; from the mirror; 404 if absent, 403 if out of scope |
| `GET /api/knox/users/:id/groups` | Bearer; `{groups, total}` the user belongs to, from the mirror; 404 if the user is absent or out of scope |
| `PUT /api/knox/users/:id` | write; target and new org must be in scope |
| `DELETE /api/knox/users/:id` | write; body `{confirmUserId}` must equal the id, else 400; 204 |
| `GET /api/knox/devices` | Bearer; optional `?q=` (model, device name, user name or id contains); `{devices, total}`; each device carries `freshness` (`fresh`, `stale`, `old`, `never` = enrolled but never connected, or `not_enrolled`; same 24h/7d bands); only devices whose owner is in scope, so unassigned devices are admin-only |
| `GET /api/knox/devices/:id` | Bearer; carries the same per-device `freshness`; 404 if absent in Knox, out of scope, or unassigned for a non-admin |
| `GET /api/knox/users/:id/devices` | Bearer; `{devices, total}` with per-device `freshness`; 404 if the user is out of scope |
| `GET /api/knox/devices/:id/detail` | Bearer; same scope/404 as the device; live from Knox (`selectDeviceInfo` detail): `{info, profiles, fetchedAt, cached, canViewLocation}`. On `knox_unreachable` the last cached answer is returned with `cached: true`; Knox business errors are not masked |
| `GET /api/knox/devices/:id/apps` | Bearer; same; `{apps: [{name, packageName, version}], fetchedAt, cached}` (up to 1000 apps) |
| `GET /api/knox/devices/:id/location?days=1\|7\|30` | write or admin only (read users get 404, as for an out-of-scope device); default 7, other values 400; `{current, history, days, showMap, fetchedAt, cached}` (`showMap` is the live general setting, never cached); cached per window; every response audited `device.location.view` `{days, cached}` |
| `GET /api/knox/groups`, `GET /api/knox/groups/:id` | Bearer; all groups for every role, including member counts |
| `POST /api/knox/groups`, `DELETE /api/knox/groups/:id` | admin only |
| `GET /api/knox/groups/:id/members` | Bearer; members filtered to scope |
| `POST`, `DELETE /api/knox/groups/:id/members` | write; `{userIds}`, every id must be in scope |
| `GET /api/admin/users`, `GET /api/admin/users/:id` | admin; app accounts |
| `POST /api/admin/users`, `PUT`, `DELETE /api/admin/users/:id` | admin; the last enabled admin cannot be removed, demoted or disabled (409) |
| `GET /api/admin/audit` | admin; `?limit=` 1-1000, default 100; `{entries}` newest first |
| `GET`, `PUT /api/admin/settings/knox` | admin; view (secret as `secretSet`, plus the region list) / save and apply live |
| `POST /api/admin/settings/knox/test` | admin; same body as PUT, saves nothing; `{ok, stage?, message}` where stage is `token` or `api` |
| `GET`, `PUT /api/admin/settings/ad` | admin; view (`bindPasswordSet`) / save and apply live |
| `POST /api/admin/settings/ad/test` | admin; `{username, password, settings?}` signs in against the given (else stored) settings; `{ok, displayName, groups, role, orgCode, access}` or `{ok:false, code, message}`; saves nothing |
| `GET`, `PUT /api/admin/settings/general` | admin; `{installationName, syncIntervalMinutes, checkUpdates, showMap}` |
| `POST /api/admin/sync` | admin; starts a sync in the background, 202 `{id, alreadyRunning}`; 503 `knox_not_configured`; audited `admin.sync.start` |
| `GET /api/admin/sync` | admin; `{running, runId?, runs}` with the last 10 runs (`id`, `trigger`, `startedAt`, `finishedAt`, `status`, `error`, `counts`) |
| any other `/api/*` | 404 JSON |
| anything else | embedded web UI, see below |

Auth error mapping:

| Condition | Status | Message / code |
|---|---|---|
| bad username or password | 401 | invalid username or password |
| invalid/expired/revoked refresh token | 401 | session expired |
| AD user in no mapped group | 403 | your account has no access to this application |
| account disabled | 403 | account disabled |
| AD cannot be reached | 503 | code `ad_unreachable` |
| AD login collides with a local row | 409 | username belongs to an account from another source |
| too many failed logins | 429 | too many failed login attempts, try again later |
| anything else | 500 | internal error (detail only in the log) |

Non-auth errors (`errorHandler`): `db.ErrNotFound` 404; `db.ErrDuplicate` and `db.ErrSourceConflict`
409; a Knox application failure (`knox.Error`, resultCode not `0`) 502 with code `knox_error` and
Knox's message; `auth.ErrForbidden` 403.

Other errors: 400 malformed login body, 401 missing/invalid bearer token, 403 failed
`RequireAdmin`/`RequireWrite` or the org scope check, 405 non-GET/HEAD to the
web UI, 413 body too large.

Web UI serving (`src/api/web.go`): GET/HEAD only. The path is cleaned; `/` is `index.html`. A
file that exists in the embedded build is served; a missing path with a file extension is 404; a
missing extension-less path gets `index.html` (SPA fallback). `_app/immutable/*` is cached for a
year, everything else is `no-cache`.

Knox requests are `application/x-www-form-urlencoded`; responses carry `resultCode` where `"0"`
means success. Per the spec the `select*` operations are `GET` (query string), and ID lists are
joined with `;`. The OAuth token is obtained by client_credentials and reused until a minute before
it expires. `knox.baseURL` is the tenant host only; the client appends `/emm/oapi`.

## First admin

Created on the setup page (see First-run setup). `knoxmgr -create-admin <username>` creates one on
demand and exits; it does not check that an admin exists afterwards, and with no admin and AD
disabled nobody can sign in.

## Web app

SvelteKit 3, SPA mode: `ssr = false`, adapter-static with `fallback: 'index.html'`, output to
`web/build`. There is no `svelte.config.js`; kit/compiler options (runes, adapter) are passed to
the `sveltekit()` plugin in `web/vite.config.ts`, which also proxies `/api` to
`127.0.0.1:8080` in `npm run dev`. Imports use the `#lib/*` alias, defined in `package.json`
`imports` as `./src/lib/*.ts` (not `$lib`). The access token is held in memory only
(`session.svelte.ts`); on page load the layout calls `/api/auth/refresh` to restore it, and the
API client retries once after a 401 via a shared refresh, then redirects to `/login`.
Components are imported with the `#components/*` alias (`./src/lib/components/*`, extension included).

UI is rendition 03 "Field coordinator" (Tailwind v4 + daisyUI, Figtree self-hosted). Themes `fclight`
and `fcdark` follow the system by default; the header toggle cycles system, light, dark and
persists light/dark in localStorage. Every page sits in the shell (header, tabs, side panel or
phone action bar registered per page with `usePanel`). Writes that the account may not do are shown
disabled with the reason.

The app name link in the header is always shown; a back arrow sits beside the `BigTitle` on detail
pages. Nav pills and clickable list rows highlight on hover (`row-hover`). The user form takes first
and last name and derives the display name from them until the display name is edited by hand.

| Route | Page |
|---|---|
| `/` | redirects to `/users` |
| `/login` | sign in |
| `/users` | user list with device badges (colour = freshness), search, select, invite |
| `/users/new` | create user |
| `/users/[id]` | user screen: details incl. last invited, groups, devices; edit, invite, delete |
| `/devices` | device list with search and a freshness badge per device |
| `/devices/[id]` | device detail: live info and profiles, applications (filterable), location with 1/7/30-day history on an embedded Leaflet map (hidden when `showMap` is off or there are no points) plus a coordinates table and OpenStreetMap links (admin and write users); "as of" note when served from cache |
| `/groups` | group list |
| `/groups/[id]` | group members |
| `/setup` | first-run wizard: code, admin, Knox (skippable), done |
| `/admin` | app accounts and audit log (admin only) |
| `/admin/settings/knox` | Knox connection form with Test and Save (admin only) |
| `/admin/settings/ad` | Active Directory settings, group mappings, test sign-in (admin only) |
| `/admin/settings/general` | installation name and sync interval (admin only) |

While Knox is not connected the Knox pages show a "Knox is not connected" notice (`KnoxOffline`)
with a link to the Knox settings for admins.

## Build order

The Go binary embeds `web/build`, so build the web first.

1. `cd web && npm ci && npm run build` (and `npm run check`)
2. `CGO_ENABLED=0 go build -o knoxmgr ./cmd/knoxmgr`
   (windows: `GOOS=windows GOARCH=amd64`, output `knoxmgr.exe`)
3. `go test ./...`

## Packaging

### Dockerfile

Multi-stage, five `FROM` lines:

1. `web` (`node:22-alpine`): `npm ci` then `npm run build` in `web/`.
2. `go` (`golang:1.26`): downloads modules, copies the source, copies `web/build` from the `web`
   stage (the Go package embeds it), then builds `knoxmgr` and `knoxmock` with `CGO_ENABLED=0`,
   `-trimpath -ldflags "-s -w"`. The version is stamped in via `-X main.version=${VERSION}` (build arg `VERSION`).
3. Target `knoxmock` (`gcr.io/distroless/static-debian12:nonroot`): only the `knoxmock` binary,
   `EXPOSE 9090`, `ENTRYPOINT knoxmock`.
4. Target `knoxmgr`, the last stage so a plain `docker build` yields it: the `knoxmgr` binary in
   `/usr/local/bin/`, an empty `/var/lib/knoxmgr` owned by `nonroot` (declared a `VOLUME` and the
   working directory, so a named volume copies that ownership), `EXPOSE 8080`.

It runs `knoxmgr` (`ENTRYPOINT` only, no default `CMD`), so with no arguments it uses the built-in
defaults and needs no config file. To use one, mount it and pass `-config` (an explicit `-config`
must exist); `example/compose.yaml` does this with `command: ["-config", "/etc/knoxmgr/config.yaml"]`.
The data dir defaults to `/var/lib/knoxmgr/data`, inside the volume.

### Root Makefile

`make help` lists targets (default). Variable `VERSION` defaults to `git describe --tags --always
--dirty`, else `dev`.

| Target | Does |
|---|---|
| `image` | two `docker build --target` runs: `knoxmgr` and `knoxmgr-mock`, each tagged `$(VERSION)` and `latest` |
| `web` | `cd web && npm ci && npm run build` (output `web/build`) |
| `build` | runs `web`, then builds `bin/knoxmgr` and `bin/knoxmock` (linux, `CGO_ENABLED=0`) |
| `build-windows` | runs `web`, then builds `bin/*.exe` for windows/amd64 |
| `desktop` | runs `web`, then builds `dist/knoxmgr-desktop-windows-amd64.exe` (`-H windowsgui`, `CGO_ENABLED=0`) |
| `test` | `go vet ./...`, `go test ./...`, `npm run check` in `web/` |
| `clean` | removes `bin/` and `dist/` |

The release workflow also builds the desktop exe and uploads it with the other binaries.

### Windows desktop (`cmd/knoxmgr-desktop`)

Windows only (`//go:build windows`). Untested on real Windows.

- Calls `server.Run` with a listener on `127.0.0.1:0` (free port) and `DataDir` = the exe's folder.
  An optional `config.yaml` next to the exe is read (`ConfigPath`); its `listenAddr` is ignored, since the
  desktop always binds 127.0.0.1 on a free port. `OnReady` receives the setup code
  instead of the log line.
- Window: `github.com/jchv/go-webview2` (WebView2 without CGO, via go-winloader). WebView2 data in
  `.\webview-data`. Creation and the message loop share one locked OS thread.
- Opens `http://127.0.0.1:<port>/`, or `/setup#code=<code>` on first run. The code sits in the URL
  fragment only: it is never logged and never sent to the server; the setup page reads it and strips
  it with `history.replaceState`.
- Log: `knoxmgr-desktop.log` (JSON, appended). Single instance: `knoxmgr-desktop.lock`, opened with
  share mode 0; a second start shows a message box and exits.
- Secure cookies stay on: browsers accept `Secure` cookies on loopback, and the HTTPS warning is
  already suppressed for `127.0.0.1`.
- WebView2 missing: opens the default browser, then a message box; OK cancels the server context.
  Closing the window does the same, so shutdown is graceful.

### example/

A local demo of knoxmgr against the fake Knox server over plain HTTP.

- `compose.yaml`: service `knoxmock` runs `knoxmgr-mock:latest`
  (`-addr 0.0.0.0:9090 -client-id demo -client-secret demo-secret`). Service `knoxmgr`
  runs `knoxmgr:latest`, depends on `knoxmock`, publishes `8088:8080`, mounts `./config.yaml` read-only
  at `/etc/knoxmgr/config.yaml` and the named volume `data` at `/var/lib/knoxmgr`. Compose does not
  build; the images must exist.
- `config.yaml`: only `secureCookies: false` (plain HTTP). Everything else is done on the setup
  page: read the setup code from `make logs`, create the admin, then connect Knox with region
  "Custom URL", `http://knoxmock:9090`, client ID `demo`, secret `demo-secret`, any tenant ID and
  "allow insecure HTTP" ticked. A data volume from before M7a must be deleted with `make reset`.
- `Makefile` (default `help`): `image` (runs the root `image` target, building both images), `up` (`docker compose up -d`),
  `down`, `logs`, `reset` (`docker compose down -v`, deletes the data volume), `admin` (runs
  `knoxmgr -create-admin $(ADMIN_USER)` in the running container; `ADMIN_USER` defaults to `admin2`).

## Status

Milestones 1 and 2 are built: config, database, authentication, embedded web skeleton with a login
page, the Knox client, Knox user / invitation / group routes with org scope checks, app account
administration and the audit log. The Knox mirror and its background sync (M7b) are in place:
screens read from the mirror. For development without a Knox tenant, `cmd/knoxmock` runs a fake
Knox Manage server (see Mock Knox above).

## Milestones

Taken from TASKS.md; TASKS.md labels only M1 and M2 explicitly, M3–M6 follow task order.

- **M1** — scaffold: module, config, db + migrations, auth (local + AD, JWT), SvelteKit skeleton
  embedded, docs.
- **M2** (built) — backend: Knox client, mock Knox server, users CRUD + invitation, user groups CRUD +
  membership, org-scoped access, audit.
- **M3** — five UI renditions, evergreen browsers, screens for M2.
- **M4** (built) — implement the chosen rendition (03 Field coordinator) and the backend enrollment
  support it needs.
- **M5** — `example/` with compose.yaml, config.yaml, Makefile (uses the mock Knox).
- **M6** — root Makefile `make image` and Dockerfile.
- **M7a** (built) — optional config, data dir, DB-stored Knox/AD/general settings with encrypted
  secrets, first-run setup code and wizard, admin settings screens.
- **M7b** (built) — Knox mirror: tables, background sync with admin "Sync now", reads from the
  mirror, write-through, `-listen`.

Releasing: `git tag vX.Y.Z && git push --tags`

## M12: group app and profile counts (backend only)

- Migration `0008_group_assign_counts.sql` adds `knox_groups.app_count` and `profile_count` (default 0).
- `knox.GroupQuery.WithCounts` sends `needDeviceUserCount=yes`; the sync fetches groups with it and `Mirror.ReplaceAll` stores the counts. Group JSON reuses the Knox names `assignAppCount` / `assignProfileCount`.
- `knoxmock` returns them (seeded groups 5/3 and 2/1, new groups 0) only when `needDeviceUserCount=yes`.
- Pending: web summary showing the counts.
