# Architecture

See [README.md](README.md) (what), [IMPLEMENTATION.md](IMPLEMENTATION.md) (how),
[QUESTIONS.md](QUESTIONS.md) (why), [CODE-BLOCKS-REFERENCE.md](CODE-BLOCKS-REFERENCE.md) (where).

## System

- One Go binary, `CGO_ENABLED=0`; serves JSON under `/api` and the embedded SvelteKit SPA elsewhere.
- Knox Manage is the source of truth; the local DB holds a mirror of its orgs, users, USER groups, memberships and devices (`knox_*` tables), plus a cache of the last live device answers (`knox_device_cache`), app accounts, refresh tokens, settings (Knox connection, AD, general; secrets encrypted), audit log.
- `config.yaml` is optional and small; a data directory (`-data`, `KNOXMGR_DATA`, default `./data`) holds the SQLite file, JWT keys and `secret.key`.
- DB: pure-Go SQLite, or Postgres. Browser talks plain HTTP to the binary; TLS is at a proxy.
- Auth: local accounts (bcrypt) or Active Directory (bind as the user). Ed25519 JWT access token + rotating refresh cookie.
- Knox client (`src/knox`) talks to the tenant; `src/knoxmock` is an in-memory fake of it for development and tests.
- `sync.Scheduler` copies Knox into the mirror at start-up, on an interval, after Knox settings are saved, and on admin request; one run at a time, a failed run leaves the mirror unchanged. Reads come from the mirror; writes go to Knox first, then patch the mirror. Groups also carry Knox-reported assigned app/profile counts (`knox_groups.app_count`, `profile_count`, set by the sync); Knox has no API listing a group's apps or profiles. The group detail screen shows the counts, bulk remove of selected members, and (admin) Delete group; the groups list only lists.
- Every authenticated request re-loads the app account from the DB; org scope is resolved from the mirror (`Mirror.SubOrgCodes`) per request.

## Module map

| Module | Path | Role |
|---|---|---|
| main | `cmd/knoxmgr` | flags, data dir, then `server.Run` |
| desktop | `cmd/knoxmgr-desktop` | Windows only: loopback listener, `server.Run`, WebView2 window, lock file, log file |
| server | `src/server` | shared start (`Options`, `Run`): config, db, migrate, wiring, serve; `-create-admin` |
| knoxmock (cmd) | `cmd/knoxmock` | runs the fake Knox server |
| config | `src/config` | optional `config.yaml` (defaults, validation, moved-key rejection), data dir, runtime `Knox`/`AD` structs, role/access constants, `Secret` |
| secretbox | `src/secretbox` | AES-GCM `Seal`/`Open` for stored secrets; `LoadOrCreate` the key file |
| settings | `src/settings` | `Manager`: validate and save Knox/AD/general settings, test connections, hold the live `knox.Client`, push AD changes into `auth.Service` |
| db | `src/db` | connection, dialect shim, migrations, all SQL (repositories, incl. `Mirror`) |
| auth | `src/auth` | login, passwords, JWT, keys, refresh rotation and replay detection, AD directory, group mapping, role checks, org `Scope`, login rate limiter, first-run `SetupCode` |
| sync | `src/sync` | `Scheduler`: Knox-to-mirror sync on a schedule and on demand, one run at a time; `Start` loop, `SyncNow`, `KnoxSaved`, `Reschedule` |
| knox | `src/knox` | typed Knox Manage client: OAuth token reuse, users, groups, orgs (cached tree), live device detail/apps/location, `knox.Error`, `Probe` (connection test) |
| knoxmock | `src/knoxmock` | in-memory fake Knox server (OAuth + the operations knoxmgr uses) |
| update | `src/update` | `Checker`: asks GitHub for the newest release tag and compares it with the running version; in-memory `Status`, no downloads |
| api | `src/api` | fiber app, handlers (auth, setup, Knox, admin, settings), setup gate, middleware, audit recording, error shape, web serving |
| web (Go) | `web/embed.go` | embeds `web/build` |
| web (app) | `web/src` | SvelteKit SPA (rendition 03): shell, users, groups, admin and login pages, API client, session |

## Dependency direction

```
cmd/knoxmgr, cmd/knoxmgr-desktop -> server
server -> api -> settings -> auth -> db
                  \        \        \      \-> config
                   \        \        \-> knox -> config
                    \        \-> secretbox
                     \-> auth, db, knox
server -> web (embed), config, secretbox, settings, sync, update
sync -> db, knox
api -> update
cmd/knoxmock -> knoxmock   (knoxmock imports no other internal package)
```

- `config` imports no internal package.
- `db` imports no internal package. `auth` imports `config` and `db`. `knox` imports `config`. `secretbox` imports none. `settings` imports `auth`, `config`, `db`, `knox` and `secretbox`. `sync` imports `db` and `knox`. `update` imports no internal package (the enabled flag arrives as a func). `api` imports `auth`, `db`, `knox`, `settings` and `update`; it sees the scheduler only through the small `api.Syncer` interface (`SyncNow`, `Running`), so it does not import `sync`. `settings` does not import `sync` either: `main` passes it callbacks with `Manager.SetHooks` (run after `SaveKnox` and `SaveGeneral`, must not block).
- `api` reaches Knox through the small `api.Knox` interface (implemented by `*knox.Client`), so tests can fake it. `Deps.Knox` is a `func() Knox` that returns nil while Knox is not configured, because the settings screens swap the client at run time; `main` adapts `settings.Manager.Knox()` (avoiding a typed-nil interface).
- Only `src/server` imports everything. No package imports `cmd` or `api`.
- Web SPA never imports Go; it calls `/api/*` only.

## Request / auth flow

Login
1. `POST /api/auth/login` -> `api.login` validates body (non-empty, length caps).
2. `auth.Service.Login`: lower-case username; local account exists -> bcrypt check only; else AD enabled -> `Directory.Authenticate` (bind as user) -> `MapGroups` -> `Users.UpsertADUser`; else invalid credentials.
3. Disabled -> `ErrAccountDisabled`. Otherwise `newSession`: sign access JWT, store hash of a new refresh token.
4. `api.respondSession`: refresh token in cookie `knoxmgr_refresh` (`Path=/api/auth`, HttpOnly, SameSite=Strict, `Secure` unless config `secureCookies: false`); body has access token and user.

Authenticated request
1. SPA sends `Authorization: Bearer <access>`.
2. `api.Authenticate` -> `Service.Verify` (signature, EdDSA, issuer, expiry) -> re-load the app account by id: missing -> 401, disabled -> 403. The `Principal` is built from the DB row, not the token claims, so role/org/disable changes apply on the next request.
3. Optional `RequireAdmin` / `RequireWrite` -> 403 `forbidden`.
4. Knox handlers build an `auth.Scope`: `Mirror.SubOrgCodes(principal org)` (admins skip this); lists are filtered in SQL by those org codes (`scopeOrgs`), orgs with `auth.FilterByOrg`, writes call `Scope.CheckWriteOrg` on every target on existing targets -> 404 "user not found" if outside scope (403 only for caller-supplied org codes).
5. The handler calls Knox, then `server.record` writes the audit row (a failed audit write is logged, not returned).

Refresh
1. SPA gets 401 -> one shared `POST /api/auth/refresh` with the cookie.
2. `Service.Refresh`: hash lookup (`GetByHash`). Unknown -> invalid token. Already revoked (replay) -> revoke all of that user's tokens, invalid token. Expired -> invalid token. Else reload user; disabled -> revoke all + 403; else revoke old, issue new session.
3. Failure clears the cookie; SPA clears session and goes to `/login`.

Login rate limit
- `api.login` asks `LoginLimiter.Allow(ip, username)` before authenticating: 5 failures in 15 minutes for that IP+normalised username (`normaliseUsername`: trimmed, lower-case, `DOMAIN\` and `@suffix` dropped), or 30 in 15 minutes for the IP alone -> 429. Only `ErrInvalidCredentials` counts as a failure, against both counters; success clears the IP+username key only. At 10000 keys expired entries go first, then the oldest by window start (oldest-first eviction). In memory, lost on restart.
- Failed logins are logged with `username` only if that account exists, else a 12-hex `userHash` (a mistyped name may be a password). The IP is `c.IP()`: the socket peer, or with `trustedProxies` set the rightmost untrusted `X-Forwarded-For` hop.

Error mapping (`api/auth.go` for auth, `errorHandler` in `api/api.go` for everything else)
- `ErrInvalidCredentials`, `ErrInvalidToken` -> 401
- `ErrNoMappedGroup`, `ErrAccountDisabled`, `auth.ErrForbidden` -> 403
- `ErrDirectoryUnreachable` -> 503, code `ad_unreachable`
- `db.ErrNotFound` -> 404; `db.ErrDuplicate`, `db.ErrSourceConflict` -> 409
- `*knox.Error` (Knox resultCode != 0, or HTTP 4xx via `Error.HTTPStatus`) -> 502, code `knox_error`, message carries Knox's text (and the status for 4xx). For HTTP 4xx the detail is admin-only: other callers get "Knox rejected the request" (the detail stays in the log). resultCode business errors are shown to everyone.
- `knox.ErrUnreachable` (transport failure, Knox 5xx, token endpoint failure, or a 401 that persists after the token refresh retry) -> 502, code `knox_unreachable`
- anything else -> 500 `internal error` (detail in log only)
- fiber errors keep their status (400 validation, 429 rate limit, 409 last admin); body shape `{error, code?}`

Devices
- `viewsOf` reads the page's devices with one `Mirror.DevicesOfUsers` query and batches `Audit.LatestInvites(ctx, ids)` (latest `invites.invited_at` per user id: `MAX ... GROUP BY user_id` over the indexed `invites` table, batched at 500 ids) for `listUsers`, `getUser` and `listMembers`. `summarize` groups enrolled devices (Knox status `A`, `B`, `BS`, `BA`, `BL`) per user into `devices {enrolled, lastConnection, freshness}`; `lastConnection` is the stalest enrolled device's and is absent if one never connected.
- Live device data (`api/device_live.go`): `GET /devices/:id/detail`, `/apps`, `/location?days=` call Knox when the screen opens, after the same scope check as `getDevice`. `liveOrCached` stores each success in `knox_device_cache` (per device and kind; location per 1/7/30-day window) and, only on `knox.ErrUnreachable`, answers from that row with `cached: true` and `fetchedAt` (the SPA shows an "as of" note). Knox business errors (`knox_error`) and an unreachable Knox with nothing cached are returned as errors. This data is not part of the bulk sync; `Mirror.ReplaceAll` prunes cache rows of devices the sync dropped.
- Location is for admin and write users: `canViewLocation`; read users get the same 404 as an out-of-scope device. Every location response is audited `device.location.view` `{days, cached}`. The response carries top-level `showMap` from the live general setting (not cached). The SPA draws the history on a Leaflet map (bundled by Vite, OSM tiles) when `showMap` is on and there are points, and keeps the coordinates table and OpenStreetMap link.
- `freshnessOf` (per user): 0 enrolled `none`; never connected or over 7 days `old`; over 24h `stale`; else `fresh`. `deviceFreshness` (per device, `api/devices.go`) adds `not_enrolled` (not an enrolled status, not graded on connection) and `never` (enrolled, never connected), otherwise the same bands. `DeviceBadge` colours: `fresh` green (`badge-success`), `stale` yellow (`badge-warning`), `old` red (`badge-error`), `never`/`not_enrolled` soft grey, `none` unstyled default badge.
- Device scope: Knox's device list has no orgCode, so the sync stores each device with its owner's org (`knox_devices.org_code`) and `listDevices` filters by it in SQL (`Mirror.Devices(orgs)`). `getDevice` and `/users/:id/devices` answer 404 when out of scope; an unassigned device has no org, so only admins see it.

Version and update check
- `main.version` (default `dev`) is set with `-ldflags -X main.version=$(VERSION)` (Makefile `VERSION` from `git describe`; Dockerfile `ARG VERSION`). `/api/health` returns it as `version`; the shell shows it.
- `update.Checker` (built in `main` with `settings.Manager.CheckUpdates` as the enabled func) checks 1 minute after start, then every 24h, and on a general-settings save (`Wake`, which checks on every wake while enabled). Disabled or a non-release version (`dev`) makes no request.
- GitHub: `releases/latest`, falling back to `/tags` (highest tag) on 404. 10s timeout, 1 MiB body cap, tag must be strict `vX.Y.Z`. The release URL is built locally from the tag, never taken from the response. Result lives in memory only; errors are short fixed strings.
- `GET /api/admin/update` returns the view (`enabled`, `current`, `latest`, `url`, `updateAvailable`, `checkedAt`, `error`). `POST /api/admin/update/check` checks now, audited `admin.update.check`; 409 `updates_disabled` when the setting is off.
- SPA: admins see `UpdateNotice` in the shell; dismissal is per version in localStorage `knoxmgr.dismissedUpdate`.

Cross-site and headers
- `POST /api/auth/refresh` and `/logout` pass `sameOrigin`: an `Origin` whose host differs from the request host -> 403; an absent `Origin` passes (SameSite=Strict covers the cookie).
- Every response gets `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, `Permissions-Policy`; `Strict-Transport-Security` (1 year) only when secure cookies are on. `/api` also gets `Cache-Control: no-store`.
- `api.New` builds the `Content-Security-Policy` once: `script-src 'self'` plus a sha256 hash of each inline `<script>` in the embedded `index.html`; it is sent on `.html` responses only. `img-src` also allows `https://tile.openstreetmap.org` for the location map.

Web serving
- Non-`/api` GET/HEAD -> embedded file; missing extension-less path -> `index.html`; missing path with extension -> 404; other methods -> 405.

First-run setup
1. Start-up finds no admin (`Users.CountAdmins` = 0), creates an `auth.SetupCode` and logs it (`first-run setup code`, field `setupCode`). It lives in memory only.
2. `setupGate` (on the `/api` group) answers everything except `/api/health` and `/api/setup/*` with 409 `setup_required` while `Setup.Required()`.
3. SPA layout reads `/api/health`; `setupRequired` sends the browser to `/setup`. `POST /api/setup/verify` checks the code (own `LoginLimiter`, 5 failures per 15 min per IP); `POST /api/setup/admin` re-checks it under `setupMu`, creates the admin, clears the code and signs in.
4. The wizard then shows the Knox form (`KnoxForm`, shared with `/admin/settings/knox`): Test (`POST .../knox/test`, `Client.Probe`) and Save.

Settings save
- `api.putKnoxSettings` -> `settings.Manager.SaveKnox`: normalise, seal a new secret with `secretbox`, `db.AppSettings.SaveKnox` (row + audit in one transaction), then swap the live `knox.Client`. AD is the same via `SaveAD` and `auth.Service.SetDirectory`. Saves are serialised by `saveMu`.
- `knoxConfigured` middleware on `/api/knox` answers 503 `knox_not_configured` while `Deps.Knox()` is nil.

## Windows desktop start (`cmd/knoxmgr-desktop`)
Lock file (`knoxmgr-desktop.lock`, share mode 0) -> log file -> listen `127.0.0.1:0` -> `server.Run` in a goroutine with `DataDir` = exe folder and optional `config.yaml` there (`listenAddr` ignored) -> `OnReady(setupCode)` -> WebView2 window on `/` or `/setup#code=<code>` (fragment only: not logged, not sent to the server; the setup page strips it) -> window closes -> cancel context -> graceful shutdown. No WebView2: default browser plus a message box, OK stops it.

## Start-up order (`server.Run`)
flags -> `config.ResolveDataDir` -> `config.Load` (optional file; rejects moved keys) -> `db.Open` -> `db.Migrate` -> (`-create-admin`: create, exit) -> JWT keys (data dir) -> `secretbox.LoadOrCreate` (`secret.key`) -> `auth.NewService` -> `settings.New` and `Load` (builds the Knox client and AD directory; an undecryptable secret is logged and leaves that part off) -> `db.NewMirror`, `sync.New`, `update.New`, `Manager.SetHooks`, `go Scheduler.Start` (first sync if Knox is configured), `go checker.Start` -> setup code if no admin -> `web.FS` -> `api.New` -> listen (`-listen`, else `KNOXMGR_LISTEN`, else `listenAddr`).
