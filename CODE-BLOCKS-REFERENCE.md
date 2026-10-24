# Code blocks reference

Block -> file -> one line. Tests: `*_test.go` beside each package. See
[ARCHITECTURE.md](ARCHITECTURE.md) for how they connect.

## cmd/knoxmgr

| Block | File | Purpose |
|---|---|---|
| `main`, `run` | `cmd/knoxmgr/main.go` | parse flags (`-config`, `-data`, `-listen` / `KNOXMGR_LISTEN`, `-create-admin`), resolve data dir, call `server.Run` with graceful shutdown on signal |
| `version` | `cmd/knoxmgr/main.go` | build version (default `dev`) set by `-ldflags -X main.version`; passed as `server.Options.Version` |

## cmd/knoxmgr-desktop

| Block | File | Purpose |
|---|---|---|
| `main`, `run` | `cmd/knoxmgr-desktop/main.go` | windows build tag; lock, log file, loopback listener, `server.Run`, WebView2 window, shutdown; errors shown in a message box |
| `version` | `cmd/knoxmgr-desktop/main.go` | build version set by `-ldflags -X main.version` |
| `acquireLock` | `cmd/knoxmgr-desktop/main.go` | open `knoxmgr-desktop.lock` with share mode 0; a second process gets a sharing violation |
| `msgBox`, `openBrowser` | `cmd/knoxmgr-desktop/main.go` | message box; default-browser fallback when WebView2 is missing |

## src/server

| Block | File | Purpose |
|---|---|---|
| `Options` | `src/server/server.go` | config path, data dir, listen address or `Listener`, version, `CreateAdmin`, `OnReady(setupCode)` (when set, the setup code is not logged) |
| `Run` | `src/server/server.go` | load config, open db, migrate, build services (auth, settings, mirror, sync, app inventory with the `OnSynced` hook, update), create the setup code, serve until the context ends |
| `createLocalAdmin` | `src/server/server.go` | `-create-admin`: create a local admin and audit it, then return |
| `adminPassword` | `src/server/server.go` | password from `KNOXMGR_ADMIN_PASSWORD`, no-echo prompt, or piped line |

## cmd/knoxmock

| Block | File | Purpose |
|---|---|---|
| `main`, `envOr` | `cmd/knoxmock/main.go` | run the fake Knox server; `-addr`, `-client-id`, `-client-secret` or `KNOXMOCK_*` env; graceful shutdown |

## src/config

| Block | File | Purpose |
|---|---|---|
| `Config`, `Database`, `JWT` | `src/config/config.go` | typed config.yaml (optional file; `DataDir` is not a key) |
| `AD`, `ADGroup`, `Knox` | `src/config/config.go` | runtime AD and Knox settings built from the database, not read from the file |
| `ResolveDataDir` | `src/config/config.go` | `-data`, else `KNOXMGR_DATA`, else `./data`; created 0700 |
| `movedKeys`, `movedJWTKeys`, `rejectMoved` | `src/config/config.go` | old keys fail load with a "moved to admin settings" message |
| `Config.TrustedProxies`, `Knox.AllowInsecureHTTP` | `src/config/config.go` | `trustedProxies` (IPs/CIDRs, validated); the runtime flag that permits an http Knox URL |
| `Config.SecureCookies`, `UseSecureCookies` | `src/config/config.go` | `secureCookies` (`*bool`); nil means true |
| `Secret` | `src/config/config.go` | string that prints as `[redacted]`; `Reveal()` to use |
| `Load` | `src/config/config.go` | read file (missing is fine unless `-config` was explicit), defaults (`:8080`, UTC, sqlite in the data dir, 15m/720h), reject moved and unknown keys, validate |
| `Validate` | `src/config/config.go` | collect every config problem |
| `Location` | `src/config/config.go` | configured timezone, UTC on error |
| role/access/driver/password-length constants | `src/config/config.go` | shared constants |

## src/db

| Block | File | Purpose |
|---|---|---|
| `Open`, `ErrNotFound`, `ErrDuplicate`, `ErrSourceConflict`, time helpers | `src/db/db.go` | connect and ping; shared errors; UTC text timestamps |
| `sqlDriverName`, `sqliteDSN`, `primaryKey`, `isUniqueViolation` | `src/db/dialect.go` | the only SQLite/Postgres differences |
| `Migrate`, `applyMigration` | `src/db/migrate.go` | apply embedded numbered migrations in transactions; per-version Go hooks run in the same transaction (0002 invites backfill) |
| schema | `src/db/migrations/0001_init.sql` | `app_users`, `refresh_tokens`, `audit_log`, `settings` |
| schema | `src/db/migrations/0002_invites.sql` | `invites` table and `invites_user_id_invited_at` index |
| schema | `src/db/migrations/0003_app_settings.sql` | `knox_settings`, `ad_settings`, `ad_group_mappings`, `general_settings` (singletons seeded with id 1; `*_cipher` columns hold sealed secrets) |
| schema | `src/db/migrations/0004_knox_mirror.sql` | `knox_orgs`, `knox_users`, `knox_groups`, `knox_group_members`, `knox_devices` (mirror, `synced_at` on each) and `sync_runs` |
| schema | `src/db/migrations/0006_update_check.sql` | `general_settings.check_updates` (0/1, default 1) |
| schema | `src/db/migrations/0007_show_map.sql` | `general_settings.show_map` (0/1, default 1) |
| schema | `src/db/migrations/0008_group_assign_counts.sql` | `knox_groups.app_count`, `profile_count` (Knox-reported assigned apps / profiles, default 0) |
| schema | `src/db/migrations/0005_device_cache.sql` | `knox_device_cache` (`device_id`, `kind`, `payload` JSON, `fetched_at`; primary key device+kind) |
| schema | `src/db/migrations/0009_app_inventory.sql` | `device_apps`, `app_categories`, `app_category_map`, `app_inventory` (singleton: `last_pull_at`, `last_report_request_at`); `general_settings.app_inventory_hours` (1-168, default 6) and `app_report_daily` (0/1, default 1); seeds 9 categories (incl. `Other`) and 66 package mappings |
| `DeviceCacheEntry`, `DeviceCache`, `PutDeviceCache` | `src/db/device_cache.go` | read / upsert the last live Knox answer of a kind for a device (`ErrNotFound` when none); `ReplaceAll` also prunes rows of dropped devices |
| `Mirror`, `NewMirror`, `Snapshot`, `MirrorOrg`, `MirrorUser`, `MirrorGroup`, `MirrorMember`, `MirrorDevice` | `src/db/mirror.go` | mirror repository and row types |
| `ReplaceAll`, `recountGroups` | `src/db/mirror.go` | replace the whole mirror from a `Snapshot` in one transaction; group user counts recomputed; `MirrorGroup` carries `AppCount` / `ProfileCount`, stored as given by `ReplaceAll` |
| `Orgs`, `Org`, `OrgMembers`, `SubOrgCodes`, `Users`, `User`, `Groups`, `Group`, `GroupMembers`, `UserGroups`, `Devices`, `Device`, `DevicesOfUsers`, `selectScoped` | `src/db/mirror.go` | reads; `orgs` arg nil = unrestricted, empty = nothing, else `org_code IN`; `SubOrgCodes` is a recursive query; every user read (`Users`, `User`, `GroupMembers`, group `user_count`) hides ids starting with `_` (`visibleUser`, `IsHiddenUser`); `api.knoxUser` 404s them for the Knox-backed write paths; `Org` is one org or `ErrNotFound`, `OrgMembers` the users directly in an org (sub-orgs excluded) |
| `UpsertOrg`, `DeleteOrg`, `MoveUsers` | `src/db/mirror.go` | org write-through patches; `UpsertOrg` also refreshes `org_name` on the org's users, `MoveUsers` sets `org_code`/`org_name` on users and `org_code` on their devices |
| `UpsertUser`, `DeleteUser`, `UpsertGroup`, `DeleteGroup`, `AddMembers`, `RemoveMembers`, `changeMembers` | `src/db/mirror.go` | write-through patches after Knox accepted a change |
| `SyncRun`, `SyncCounts`, `StartRun`, `FinishRun`, `RecentRuns`, `LastSuccess` | `src/db/mirror.go` | `sync_runs` history |
| schema | `src/db/migrations/0010_app_inventory_fixes.sql` | adds `device_apps.mandatory`; deletes `knox_device_cache` rows of kind `apps`; seeds Messaging (tachyon, `com.Slack`), Productivity (Authenticator, Acrobat) and Entertainment (Shazam, Candy Crush) mappings |
| `IsEnrolled` | `src/knox/enrolled.go` | the single definition of an enrolled device status, used by `api` and `sync` |
| `Apps`, `NewApps`, `DeviceAppRow`, `DeviceApp`, `AppFilter`, `AppSummary`, `AppDevice`, `Category`, `InventoryState`, `SystemCategory`, `ErrInvalidCategoryName` | `src/db/apps.go` | app inventory and category repository; `orgs` args follow `Mirror` (nil = unrestricted, empty = nothing) |
| `ReplaceDeviceApps`, `DeviceApps`, `MarkUninstallRequested` | `src/db/apps.go` | replace one device's app rows in a transaction (clears the uninstall mark); list a device's apps with resolved category (`System` from the system flag, else mapped, else `Other`); mark a row uninstall requested |
| `Apps` (method), `App`, `AppDevices`, `summaries`, `appNames`, `pickNames`, `nameCount`, `scoped`, `appScopeFrom` | `src/db/apps.go` | in-scope app list (search, category, system filters), one app, and its in-scope devices; `Name` is the majority `appName` across in-scope devices (ties alphabetical), `AlternateNames` the other non-empty names (count desc, then alpha); scope is the owner's org, so unassigned devices are admin-only |
| `Categories`, `CreateCategory`, `RenameCategory`, `DeleteCategory`, `SetAppCategory`, `categoryName` | `src/db/apps.go` | category CRUD (`System` reserved, 100-byte cap, unique names; delete cascades the mappings); `SetAppCategory` maps a package, nil clears, returns the previous id |
| `Inventory` (method), `SetLastPull`, `SetLastReportRequest`, `parseOptTime` | `src/db/apps.go` | persisted pull / report-request times |
| `User`, `Users` (`Create`, `GetByID`, `GetByUsername`, `Update`, `UpsertADUser`, `CountAdmins`, `List`, `Delete`) | `src/db/users.go` | app account repository; `Delete` nulls `invites.invited_by`; AD upsert never overwrites a local row (`ErrSourceConflict`) |
| `RefreshTokens` (`Insert`, `GetValid`, `GetByHash`, `Revoke`, `RevokeAllForUser`, `RevokeAllForUserExcept`) | `src/db/refresh_tokens.go` | hashed refresh token store; single-use revoke; `GetByHash` finds revoked tokens so replay can be detected; `RevokeAllForUserExcept` keeps one token hash (password change) |
| `AuditEntry`, `Audit` (`Insert`, `InsertInvites`, `ListRecent`, `LatestInvites`), `insertAudit` | `src/db/audit.go` | append-only audit log; `LatestInvites(ctx, ids)` maps user id to its latest invite time (`MAX(invited_at)` from `invites`, batched at 500); `InsertInvites(ctx, e, userIDs)` writes the audit row and one `invites` row per id in one transaction; `insertAudit` works inside a transaction |
| `Settings` (`Get`, `Set`) | `src/db/settings.go` | key/value settings; `Set` audits atomically |
| `AppSettings` (`GetKnox`, `GetAD`, `GetGeneral`, `SaveKnox`, `SaveAD`, `SaveGeneral`), `KnoxSettings`, `ADSettings`, `ADMapping`, `GeneralSettings` (incl. `CheckUpdates`, `ShowMap`, `AppInventoryHours`, `AppReportDaily`) | `src/db/app_settings.go` | admin-editable settings; every save writes its `settings.knox`/`settings.ad`/`settings.general` audit row in the same transaction; secrets audited only as changed/unchanged |

## src/secretbox

| Block | File | Purpose |
|---|---|---|
| `Box`, `New`, `Seal`, `Open`, `ErrInvalid` | `src/secretbox/secretbox.go` | AES-256-GCM with a random nonce; base64(nonce||ciphertext) |
| `LoadOrCreate` | `src/secretbox/secretbox.go` | read `secret.key`, or create it (random, 0600, exclusive) |

## src/settings

| Block | File | Purpose |
|---|---|---|
| `Manager`, `New`, `Load`, `Knox`, `InstallationName` | `src/settings/manager.go` | holds the live `*knox.Client`; `Load` builds Knox and AD from the database, logging (not failing) on an unreadable secret |
| `applyKnox`, `applyAD`, `knoxConfig`, `adConfig` | `src/settings/manager.go` | swap the live Knox client / `auth.Service` directory; token URL derived from the base URL |
| `KnoxView`, `GetKnox`, `SaveKnox`, `TestKnox`, `KnoxTestResult`, `resolveKnox` | `src/settings/manager.go` | Knox settings view (no secret), save-and-apply, connection test via `knox.Client.Probe` |
| `ADView`, `GetAD`, `SaveAD`, `TestAD`, `ADTestSuccess`, `ADTestFailure`, `resolveAD` | `src/settings/manager.go` | AD settings view, save-and-apply, test sign-in with role result |
| `GetGeneral`, `SaveGeneral` | `src/settings/manager.go` | installation name and sync interval |
| `CheckUpdates` | `src/settings/manager.go` | whether GitHub update checks are allowed (live general setting) |
| `ShowMap` | `src/settings/manager.go` | whether the device location map is shown (live general setting; `GeneralInput.ShowMap` nil keeps the stored value) |
| `AppInventoryHours`, `AppReportDaily` | `src/settings/manager.go` | live general settings read by `sync.Inventory` (hours 1-168; `GeneralInput.AppReportDaily` nil keeps the stored value) |
| `SetHooks`, `onKnoxSaved`, `onGeneralSaved` | `src/settings/manager.go` | callbacks run after a successful `SaveKnox` / `SaveGeneral` (main wires the sync scheduler); must not block |
| `KnoxInput`, `ADInput`, `ADMapping`, `GeneralInput`, `normalise`, `InvalidError`, `Region`, `Regions` | `src/settings/validate.go` | input validation (400 via `InvalidError`), preset Knox regions |

## src/auth

| Block | File | Purpose |
|---|---|---|
| `Service`, `NewService`, `Login`, `loginAD`, `Refresh` (replay of a revoked token revokes all the user's tokens), `Logout`, `LogoutAll`, `Verify`, `newSession` | `src/auth/service.go` | login (local first, then AD), refresh rotation, session issue |
| `LogoutOthers` | `src/auth/service.go` | revoke all of a user's refresh tokens except the caller's (after a password change) |
| `Session`, `newOpaqueToken`, `hashToken` | `src/auth/service.go` | session result; random refresh token and its SHA-256 |
| `Tokens`, `NewTokens`, `Issue`, `Verify`, `claims`, `ErrInvalidToken` | `src/auth/jwt.go` | Ed25519 JWT access tokens |
| `LoadOrGenerateKeys` and PEM helpers | `src/auth/keys.go` | load or create the 0600 key pair |
| `HashPassword`, `CheckPassword` | `src/auth/password.go` | bcrypt with length bounds and constant-ish timing for unknown users |
| `Directory`, `DirectoryUser`, `ADDirectory`, `NewADDirectory`, auth errors | `src/auth/directory.go` | AD seam over go-ad-auth; maps network errors to `ErrDirectoryUnreachable` |
| `Grant`, `MapGroups`, `groupNames` | `src/auth/groups.go` | AD group to role/org/access; admin wins, else first user mapping |
| `Principal`, `RequireAdmin`, `RequireWrite`, `ErrForbidden` | `src/auth/principal.go` | caller identity and role checks |
| `Scope`, `NewScope`, `CanReadOrg`, `CheckWriteOrg`, `CanManageGroups`, `FilterOrgs`, `FilterByOrg` | `src/auth/scope.go` | the one org-scope decision point; org codes come from Knox |
| `LoginLimiter`, `NewLoginLimiter`, `Allow`, `Failure`, `Success`, `ErrRateLimited` | `src/auth/ratelimit.go` | in-memory failed-login counters per IP+username (5 per 15m) and per IP (30 per 15m); `Success` clears the first only |
| `normaliseUsername`, `limiterKey` | `src/auth/ratelimit.go` | one counter for `DOMAIN\Bob`, `bob@corp`, ` BOB ` |
| `exhausted`, `bump`, `makeRoom` | `src/auth/ratelimit.go` | limit check; count a failure; at 10000 keys drop expired, then the oldest 10% |
| `SetupCode`, `NewSetupCode`, `String`, `Required`, `Verify`, `Clear` | `src/auth/setup.go` | in-memory one-time first-run code (12 symbols, constant-time compare); nil means not required |
| `SetDirectory` | `src/auth/service.go` | swap the AD directory and mappings live (atomic) |

## src/knox

| Block | File | Purpose |
|---|---|---|
| `Client`, `New`, `bearer`, `drop`, `send` | `src/knox/client.go` | Knox client; OAuth client_credentials token reused until near expiry; `baseURL` is the host, `/emm/oapi` is appended |
| `Error` | `src/knox/client.go` | application-level failure: resultCode != "0", or HTTP 4xx (`HTTPStatus` set; API maps to 502 `knox_error`; 5xx/transport/token failures are `ErrUnreachable` -> `knox_unreachable`) |
| `envelope`, `call`, `joinIDs`, `set` | `src/knox/client.go` | generic request/decode; `;`-joined id lists; optional form fields |
| `User`, `UserPage`, `Group`, `GroupPage`, `Organization`, `Count` | `src/knox/types.go` | Knox payloads; `Count` accepts number, string or a `{total}` object; `DeviceCount` reads the spec object: enrolled, falling back to total |
| `Device`, `DevicePage`, `DeviceQuery`, `SelectDeviceList`, `SelectDeviceInfo`, `SelectDevicesByUser` | `src/knox/devices.go`, `src/knox/types.go` | device operations; `SelectDevicesByUser` assumes a `devicelist` wrapper |
| `DeviceDetail`, `DeviceApp` (with `System`, `Managed`, `Mandatory`, `flag`), `DeviceLocation`, `SelectDeviceDetail`, `flexString` (string/number/bool/null read as text), `SelectDeviceAppList` (pages of 1000, at most 10; reads real Knox's `appList`, falls back to the spec's `devicelist`; returns an `AppListDiagnostic` of key names/types when Knox sent no apps, else the first item's key names/types), `itemShape`, `AppListDiagnostic`, `jsonType`, `diagnoseAppList`, `SelectDeviceLocation`, `SelectDeviceLocationHistory`, `requireDeviceID` | `src/knox/device_detail.go` | live per-device calls (send `deviceId` only); history takes whole days (`period`), rounded up; Knox has no agent/client version, so `AgentProfileName`/`ClientProfileName` are used |
| `UninstallApp`, `SyncInstalledAppList`, `ValidPackage` | `src/knox/app_commands.go` | device commands `sendDeviceControlForUninstallApp` / `...SyncInstalledAppList`; package names validated before sending; `knoxId` optional |
| `UserQuery`, `UserInput`, `SelectUsers`, `SelectUserWithID`, `CreateUser`, `UpdateUser`, `PurgeUser`, `RequestEnrollment`, `EnrollmentResult` | `src/knox/users.go` | user operations |
| `GroupQuery.WithCounts` | `src/knox/groups.go` | sends `needDeviceUserCount=yes`; the sync fetches groups with it to get `assignAppCount` / `assignProfileCount` |
| `GroupQuery`, `SelectGroups`, `InsertGroup`, `DeleteGroup`, `SelectGroupUsers`, `InsertGroupUnits`, `DeleteGroupUnits`, `groupUnits` | `src/knox/groups.go` | USER group operations and membership; `InsertGroupUnits`, `DeleteGroupUnits` and `DeleteGroup` always send `applyProfile=1` so Knox pushes profiles itself |
| `CreateOrg`, `UpdateOrg`, `DeleteOrg`, `AddOrgMembers`, `orgMutation` | `src/knox/orgs.go` | org writes (`/org/create`, `update`, `delete`, `addMembers`); `orgMutation` posts and drops the cached org list; `AddOrgMembers` sends `userIds` as `['u1','u2']` |
| `Probe`, `ProbeError`, `tokenStatusError`, `transportFailure` | `src/knox/probe.go` | connection test: token, then one org list; failures in words fit for an admin, no URLs or secrets |
| `SelectOrgList`, `SelectOrg`, `SelectOrgUsers`, `Orgs`, `SubOrgCodes` | `src/knox/orgs.go` | `Orgs` is the exported cached (60s) org list, used by `GET /api/knox/orgs` and `SubOrgCodes`; `SubOrgCodes` is an org plus descendants |

## src/sync

| Block | File | Purpose |
|---|---|---|
| `Scheduler`, `New`, `Start`, `tick`, `interval` | `src/sync/sync.go` | loop: sync at start-up, every configured interval, on Knox save; wake-ups restart the countdown; falls back to 15m |
| `SyncNow`, `Running`, `begin`, `run`, `ErrNotConfigured` | `src/sync/sync.go` | single-flight run in the background (records a `sync_runs` row; failure keeps the mirror) |
| `KnoxSaved`, `Reschedule`, `signal` | `src/sync/sync.go` | non-blocking wake-ups for the loop |
| `OnSynced` | `src/sync/sync.go` | hook called after every successful run with its trigger; server wires it to `Inventory.AfterSync` |
| `Inventory`, `NewInventory`, `ErrInventoryRunning` | `src/sync/apps.go` | keeps the mirrored app lists fresh; `hours` / `daily` funcs come from `settings.Manager` |
| `PullDevice`, `pullDevice`, `PullAll`, `forEach`, `enrolledDevices` | `src/sync/apps.go` | per-device replace from Knox; full pull over enrolled devices with 4 workers, single-flight, per-device failures logged and counted; the summary line carries `appRows` and `devicesWithoutApps`, and one warn per pull samples the payload shape (key names and types, never values) of an empty answer |
| `AfterSync`, `PullIfDue`, `reportIfDue`, `requestReports`, `Start` | `src/sync/apps.go` | manual sync always pulls, others when `hours` have passed; once a day asks enrolled Android devices to report (one `device.apps.report_request` audit row); `Start` re-checks every minute from persisted times |
| `fetch`, `collect` | `src/sync/sync.go` | read orgs, users, USER groups with members, devices from Knox (1000 per page, 20 pages max) into a `db.Snapshot`; owner org copied onto devices |

## src/update

| Block | File | Purpose |
|---|---|---|
| `tagRe`, `currentRe`, `semver`, `parse`, `compare` | `src/update/semver.go` | strict `vX.Y.Z` release tags; running version may be a git-describe string (base counts) |
| `Checker`, `New`, `Status`, `Wake`, `Start`, `run` | `src/update/checker.go` | in-memory result; loop 1 min after start then 24h; `Wake` checks soon on every wake while enabled |
| `Check`, `ErrDisabled`, `store` | `src/update/checker.go` | one check now; off -> `ErrDisabled`; non-release version is not queried |
| `latestTag`, `get` | `src/update/checker.go` | `releases/latest`, `/tags` fallback; 10s timeout, 1 MiB cap, fixed error texts; URL built locally |

## src/knoxmock

| Block | File | Purpose |
|---|---|---|
| `Server`, `New`, `ServeHTTP`, `token`, `requireBearer`, `ok`, `fail`, `writeJSON` | `src/knoxmock/server.go` | routes, OAuth token endpoint, bearer check, Knox envelope; error codes 1001-1003 |
| `seed`, `org`, `user`, `group`, `userView`, `within` | `src/knoxmock/data.go` | seeded org tree (ROOT, SALES, SUPPORT, SALES_EMEA), 6 users, 2 groups |
| `selectUsers`, `selectUserWithID`, `createUser`, `updateUser`, `purgeUser`, `requestEnrollment`, `applyUserForm`, `splitIDs`, `page`, `window` | `src/knoxmock/handlers_user.go` | user endpoints |
| `selectDeviceList`, `selectDeviceInfo`, `selectDevicesByUser` | `src/knoxmock/handlers_device.go` | device endpoints |
| `seedDevices`, `device`, `deviceView`, `deviceViews` | `src/knoxmock/data.go` | 6 seeded devices covering every freshness band, an unassigned and a non-enrolled one |
| `selectDeviceAppList` (paged; answers like real Knox: `appList` and a numeric `total`), `selectDeviceLocation`, `selectDeviceLocationHistory`, `knownDevice` | `src/knoxmock/handlers_device.go` | live device endpoints; `period` 1-30 days, `knownDevice` resolves `deviceId` or writes the error |
| `androidDevice`, `uninstallApp`, `syncInstalledAppList` | `src/knoxmock/handlers_device.go` | app commands: Android devices only; uninstall removes the app from the device's list (persisting), refuses system apps (`1901013`) and unknown packages; the report request only checks the device |
| `deviceApps`, `locationHistory`, `locationDays` | `src/knoxmock/data.go` | generated apps (25-40 per enrolled Android device, about 12 system; `systemApp`/`isManaged` as `Y`/`N`; `com.google.android.apps.photos` is "Google Photos" on most devices and "Fotos" on some, to show alternate names) and location trail per device (30 days back) |
| `selectGroups`, `insertGroup`, `deleteGroup`, `selectGroupUsers`, `insertGroupUnits`, `deleteGroupUnits`, `changeUnits` | `src/knoxmock/handlers_group.go` | group endpoints (`changeUnits` and `deleteGroup` reject unless `applyProfile=1`); with `needDeviceUserCount=yes` each group also carries `assignAppCount` / `assignProfileCount` (seeded 5/3 and 2/1, new groups 0) |
| `orgSelectList`, `orgSelect`, `orgSelectUsers`, `orgCreate`, `orgUpdate`, `orgDelete`, `orgAddMembers` | `src/knoxmock/handlers_org.go` | org endpoints, reads and writes |

## src/api

| Block | File | Purpose |
|---|---|---|
| `Knox` (interface), `Syncer` (interface), `Inventory` (interface: `PullDevice`), `Deps`, `New` | `src/api/api.go` | build fiber app and routes; `Knox` is the slice of `*knox.Client` the handlers use (incl. `UninstallApp`, `SyncInstalledAppList`); `Deps.Knox` is `func() Knox` (nil while unconfigured), plus `Settings`, `Setup`, `SetupLimiter`, `Mirror` (`*db.Mirror`), `Apps` (`*db.Apps`), `Inventory`, `Sync` (`Syncer`: `SyncNow`, `Running`), `Update` (`*update.Checker`), `Version` |
| `setupGate`, `knoxConfigured` | `src/api/api.go` | 409 `setup_required` until an admin exists (health and `/api/setup/*` exempt); 503 `knox_not_configured` on `/api/knox` |
| `errorHandler`, `errorBody` | `src/api/api.go` | JSON error shape; maps not found 404, duplicate/source conflict 409, `knox.Error` 502, `knox.ErrUnreachable` 502 `knox_unreachable`; 500 detail only logged |
| `health`, `requestLog` | `src/api/api.go` | `{status, name, version, knoxConfigured, setupRequired}`, request logging |
| `setupVerify`, `setupAdmin`, `checkSetupCode`, `fiberError`, `errSetupDone` | `src/api/setup.go` | `/api/setup` endpoints; `setupMu` makes check-and-create atomic; separate rate limiter |
| `settingsRoutes`, `getKnoxSettings`, `putKnoxSettings`, `testKnoxSettings`, `getADSettings`, `putADSettings`, `testADSettings`, `getGeneralSettings`, `putGeneralSettings` | `src/api/settings.go` | `/api/admin/settings/*` (admin only) |
| `server.securityHeaders` | `src/api/api.go` | nosniff, frame, referrer, Permissions-Policy headers; HSTS when secure cookies |
| `noStore` | `src/api/api.go` | `Cache-Control: no-store` on the `/api` group |
| `sameOrigin` | `src/api/api.go` | 403 on refresh/logout when `Origin` host differs from the request host; absent passes |
| `Deps.TrustedProxies` | `src/api/api.go` | enables fiber proxy trust and IP validation for the client IP |
| `login`, `refresh`, `logout`, `me` | `src/api/auth.go` | auth endpoints; `login` applies the rate limiter (429) |
| `updateAccount`, `changePassword`, `selfLocal`, `errADManaged` | `src/api/account.go` | self-service `PUT /api/auth/me` (display name) and `POST /api/auth/me/password` (`sameOrigin`); AD accounts 403 `ad_managed`; wrong current password 403 `wrong_password` and counts against `LoginLimiter`; revokes other sessions; audits `account.update` / `account.password` (no secrets) |
| `TestUpdateAccount`, `TestAccountADRejected`, `TestChangePassword`, `TestChangePasswordRevokesOtherSessions`, `TestChangePasswordRateLimited`, `TestChangePasswordAuditHasNoSecrets` | `src/api/account_test.go` | account endpoint tests |
| `logLoginFailure` | `src/api/auth.go` | failed-login log: username if the account exists, else 12-hex `userHash` |
| `respondSession`, `clearCookie`, cookie constants | `src/api/auth.go` | refresh cookie `knoxmgr_refresh` |
| `authStatus`, `authError` | `src/api/auth.go` | auth error to 401/403/409/503/500 mapping |
| `userBody`, `authBody`, `toUserBody` | `src/api/auth.go` | response JSON |
| `Authenticate`, `RequireAdmin`, `RequireWrite`, `require`, `principalFrom` | `src/api/middleware.go` | Bearer verification, per-request account re-load (401 unknown, 403 disabled), role gates |
| `knoxRoutes`, `knoxUser`, `knoxGroup`, `targetsInWriteScope` | `src/api/knox.go` | `/api/knox` routes; per-request `Scope`; scope-checked lookups |
| `userFields`, `fieldsOf` | `src/api/knox.go` | editable user fields and validation |
| `userView`, `viewsOf`, `listOrgs` | `src/api/knox.go` | user JSON with `devices`/`invitedAt`; `viewsOf` attaches them to a user slice with one mirror device query and one batched invite lookup; `GET /api/knox/orgs` from the mirror, filtered to scope |
| `mirrorOrg`, `orgFields`, `validateOrg`, `getOrg`, `createOrg`, `updateOrg`, `deleteOrg`, `addOrgMembers` | `src/api/orgs.go` | org handlers: `GET /orgs/:code` (scope-checked, with direct members), admin-only create / update / delete / move members; `validateOrg` needs a name (`maxOrgName`) and an existing parent that is not the org or its descendant; knox first, then mirror; audited `knox.org.*` |
| `listUserGroups`, `listUsers`, `getUser`, `createUser`, `updateUser`, `purgeUser`, `inviteUsers` | `src/api/knox.go` | user handlers (`createUser` rejects ids starting with `_`); reads from the mirror (`listUserGroups` is `GET /users/:id/groups`), scoped in SQL; create/update/purge patch the mirror after Knox succeeds; purge needs `confirmUserId` |
| `listGroups`, `getGroup`, `createGroup`, `deleteGroup`, `listMembers`, `addMembers`, `removeMembers`, `changeMembers` | `src/api/knox.go` | group handlers; groups visible to all, members filtered to scope in SQL; create/delete/membership changes patch the mirror after Knox succeeds; `listMembers` returns each member with `devices` |
| `freshnessOf`, `deviceFreshness`, `isEnrolled`, `deviceView`, `deviceSummary`, `viewOfDevice`, `summarize`, `listDevices`, `getDevice`, `listUserDevices` | `src/api/devices.go` | freshness constants (24h stale, 7d old), enrolled statuses, device JSON and the three device handlers; read from the mirror; scope via the owner's org in SQL, out of scope 404 |
| `liveOrCached`, `deviceInfo`, `detailPayload`, `detailResponse`, `detailOf`, `rfc3339`, `canViewLocation`, `deviceDetail`, `locationPoint`, `locationPayload`, `pointOf`, `locationDays`, `deviceLocation` | `src/api/device_live.go` | live device detail/location with cache fallback on `knox_unreachable`; location admin/write only (404 otherwise), validated `days`, audited `device.location.view` |
| `appRoutes`, `appAdminRoutes`, `appSummaryView` (incl. `alternateNames`, never null), `appDeviceView`, `appPackage`, `listApps`, `getApp` | `src/api/apps.go` | `/api/knox/apps`, `/apps/:package`, `/app-categories`, `/devices/:id/apps*` (every role, scoped) and the admin category routes; the package param must pass `knox.ValidPackage` |
| `protectedReason`, `errAppProtected` | `src/api/apps.go` | why an app may not be uninstalled: only the built-in Knox Manage agent packages/prefixes; uninstall answers 403 `app_protected` |
| `uninstallApp`, `uninstallOne`, `uninstallResult` | `src/api/apps.go` | `POST /apps/:package/uninstall` (write): per-device scope and write check, Android, installed (Knox decides about system apps); one Knox call and one `device.app.uninstall` audit row per device; per-device result |
| `deviceApps`, `deviceAppView`, `refreshDeviceApps` | `src/api/apps.go` | `GET /devices/:id/apps`: pull that device (Android) then answer from the mirror, `stale` when the pull failed, `syncedAt` falls back to the global last pull; `POST .../apps/refresh` (write) asks the device to report, audited `device.apps.refresh` |
| `categoryView`, `categories`, `category`, `listAppCategories`, `categoryID`, `categoryNameBody`, `createAppCategory`, `renameAppCategory`, `deleteAppCategory`, `setAppCategory` | `src/api/apps.go` | category list (all roles) and admin changes, audited `app.category.create` / `rename` / `delete` / `assign` |
| `scope`, `scopeOrgs`, `mirrorUser`, `mirrorGroup`, `errGroupNotFound`, `mirrorWrite`, `refreshMirrorUser`, `userOfMirror`, `usersOfMirror`, `mirrorOfUser`, `orgsOfMirror`, `groupOfMirror`, `groupsOfMirror`, `devicesOfMirror` | `src/api/mirror.go` | caller's `Scope` and SQL org list from the mirror (nil = admin); mirror reads that look like not-found when absent; mirror rows to the `knox.*` JSON shapes; post-Knox mirror updates log, not fail |
| `startSync`, `syncStatus`, `syncRunView`, `viewOfRun`, `syncHistory` | `src/api/sync.go` | `POST /api/admin/sync` (202, audited `admin.sync.start`) and `GET /api/admin/sync` (running flag, last 10 runs) |
| `updateView`, `server.updateView`, `updateStatus`, `updateCheck` | `src/api/update.go` | `GET /api/admin/update`; `POST /api/admin/update/check` (409 `updates_disabled`, audited `admin.update.check`) |
| `adminRoutes`, `appUser`, `adminListUsers`, `adminGetUser`, `adminCreateUser`, `adminUpdateUser`, `adminDeleteUser`, `adminAudit`, `checkRoleShape`, `otherEnabledAdmin`, `errLastAdmin` | `src/api/admin.go` | `/api/admin` app-account CRUD and audit listing (admin only); last enabled admin is protected |
| `record`, `jsonPtr` | `src/api/audit.go` | write an audit row after a mutation; failure logged only |
| `bind`, `badRequest`, `checkPattern`, `checkText`, `checkEmail`, `checkIDList`, id regexps | `src/api/validate.go` | request validation |
| `serveWeb` | `src/api/web.go` | embedded UI; SPA fallback to index.html unless under `/_app/` or a known static extension (404), cache headers, CSP on `.html` |
| web tests | `src/api/web_test.go` | unknown `/api/*` paths return JSON 404; SPA fallback and static rules |
| `contentSecurityPolicy`, `inlineScript` | `src/api/web.go` | CSP with a sha256 per inline script of `index.html`, computed at start-up |
| service worker | `web/src/service-worker/index.ts` | PWA shell cache: build output and static assets only, network-first navigations, `/api/` never cached |
| `TestSecurityHeaders` and others | `src/api/api_hardening_test.go` | headers, HSTS, CSP, trusted-proxy client IP, cross-origin refusal, Knox 4xx admin-only, failed-login log |

## Dockerfile

| Block | File | Purpose |
|---|---|---|
| `ARG VERSION` | `Dockerfile` | build arg injected as `-X main.version`; Makefile `VERSION` (git describe) feeds it and the plain `go build` targets |
| stage `knoxmock` | `Dockerfile` | image with only `knoxmock`, `EXPOSE 9090` |
| stage `knoxmgr` | `Dockerfile` | last stage (default for plain `docker build`): `knoxmgr`, and `/var/lib/knoxmgr` as working dir and volume (data dir `data/` beneath it) |

## Release build

| Block | File | Purpose |
|---|---|---|
| `desktop` target | `Makefile` | `dist/knoxmgr-desktop-windows-amd64.exe` with `-H windowsgui`; `clean` also removes `dist/` |
| desktop build step | `.github/workflows/release.yml` | builds the same exe on release and uploads it |

## web (Go)

| Block | File | Purpose |
|---|---|---|
| `FS` | `web/embed.go` | `web/build` embedded as an `fs.FS` |

## web/src

| Block | File | Purpose |
|---|---|---|
| `ApiError`, `api`, `refresh` | `web/src/lib/api/client.ts` | fetch wrapper: bearer header, one shared refresh on 401, redirect to `/login` |
| `login`, `logout`, `restore` | `web/src/lib/api/auth.ts` | auth calls; `restore` restores session from the refresh cookie |
| `updateMe`, `changePassword` | `web/src/lib/api/account.ts` | `/api/auth/me` and `/api/auth/me/password` calls |
| `session` | `web/src/lib/api/session.svelte.ts` | in-memory access token and current user (runes) |
| `User`, `AuthResponse`, `Health`, `ApiErrorBody`, `Role` | `web/src/lib/api/types.ts` | API types; `Health` has `knoxConfigured`, `setupRequired` |
| `verifySetupCode`, `createFirstAdmin` | `web/src/lib/api/setup.ts` | `/api/setup` calls; the second signs in |
| `getKnoxSettings`, `saveKnoxSettings`, `testKnoxSettings`, `getAdSettings`, `saveAdSettings`, `testAdSignIn`, `getGeneralSettings`, `saveGeneralSettings`, `getUpdateStatus`, `checkForUpdates` | `web/src/lib/api/settings.ts` | `/api/admin/settings` calls; the last two call `/api/admin/update` and `/update/check` |
| `UpdateStatus` | `web/src/lib/api/types.ts` | update view type; `Health.version`, `GeneralSettings.checkUpdates` |
| `UpdateNotice` | `web/src/lib/components/UpdateNotice.svelte` | admin-only new-version banner; dismissed per version in localStorage `knoxmgr.dismissedUpdate` |
| `knoxStatus` | `web/src/lib/health.svelte.ts` | whether Knox is connected; set from health, a 503 `knox_not_configured` and a saved Knox form |
| `KnoxForm` | `web/src/lib/components/KnoxForm.svelte` | Knox connection form with Test and Save, shared by the setup wizard and the Knox settings page |
| `KnoxOffline` | `web/src/lib/components/KnoxOffline.svelte` | "Knox is not connected" notice with a link for admins |
| `Organization`, `OrgDetail`, `KnoxUser`, `Group`, `AppUser`, and request types | `web/src/lib/api/types.ts` | API types; `KnoxUser.devices` |
| `Device`, `Freshness`, `listDevices`, `getDevice`, `listUserDevices`, `DeviceDetail`, `DeviceLocation`, `GeoPoint`, `getDeviceDetail`, `getDeviceLocation` | `web/src/lib/api/devices.ts` | `/api/knox` device calls, incl. live detail/location |
| `AppSummary`, `AppDevice`, `AppCategory`, `UninstallResult`, `DeviceAppRow`, `DeviceAppsResponse`, `listApps`, `getApp`, `uninstallApp`, `listAppCategories`, `createAppCategory`, `renameAppCategory`, `deleteAppCategory`, `setAppCategory`, `getDeviceApps`, `refreshDeviceApps`, `compareCategories` | `web/src/lib/api/apps.ts` | `/api/knox/apps*`, `/api/knox/app-categories`, `/api/knox/devices/:id/apps*` and the admin category calls |
| `listOrgs`, `getOrg`, `createOrg`, `updateOrg`, `deleteOrg`, `moveUsersToOrg`, `listUsers`, `getUser`, `createUser`, `updateUser`, `purgeUser`, `inviteUsers`, group calls | `web/src/lib/api/knox.ts` | `/api/knox` calls |
| `listAppUsers`, `createAppUser`, `updateAppUser`, `deleteAppUser`, `listAudit` | `web/src/lib/api/admin.ts` | `/api/admin` calls |
| `theme` | `web/src/lib/theme.svelte.ts` | system/light/dark cycle; `fclight`/`fcdark`; persisted in localStorage |
| `accessOf`, `displayName`, `ownerLinkable` | `web/src/lib/perms.ts` | what the session user may write, with the reason when not; `ownerLinkable` is false for hidden `_` owners, which render as plain text instead of a user link |
| `shell`, `usePanel` | `web/src/lib/shell.svelte.ts` | header title/back and per-page side panel / phone action bar |
| orgs store | `web/src/lib/orgs.svelte.ts` | cached org tree for pickers and names; `refresh()` reloads after an org change |
| `toast` | `web/src/lib/toast.svelte.ts` | transient messages |
| `Shell`, `BigTitle`, `Toast` | `web/src/lib/components/` | page frame (nav incl. Apps and Orgs for every role; shows `Version x` from health and `UpdateNotice` for admins; account menu with Edit account and Sign out rows), collapsing title (optional right-aligned `actions` snippet), toast host |
| `Dialog`, `ConfirmDelete`, `InviteResultDialog` | `web/src/lib/components/` | modals; purge asks for the typed user id |
| `UserDetail` | `web/src/lib/components/UserDetail.svelte` | user screen: details, groups (filter when >8, searchable add), devices; Edit/Invite/Delete in the BigTitle `actions` snippet |
| `SearchPicker` | `web/src/lib/components/SearchPicker.svelte` | type-to-filter listbox: arrow keys, Enter picks, Escape clears; props items, label, onpick, placeholder, disabled |
| `UserEditor`, `Field`, `OrgSelect`, `Note` | `web/src/lib/components/` | user create/edit form and its parts |
| `DeviceApps`, `DeviceLocation` | `web/src/lib/components/` | device screen sections: app list from the mirror (`getDeviceApps`) grouped by category with an `AppIcon`, system apps hidden by default, "as of" time and a stale note, Refresh (write) and a confirmed Uninstall per app; location (current fix, 1/7/30-day history on a bundled Leaflet map with OSM tiles, hidden when `showMap` is off or no points; coordinates table, OpenStreetMap links); location shows an "as of" note when cached |
| `AppIcon` | `web/src/lib/components/AppIcon.svelte` | offline monogram tile (`sm`/`md`/`lg`); hue hashed from the package, so no icon requests |
| `DeviceBadge`, `Chip`, `Avatar`, `Icon` | `web/src/lib/components/` | small display components (`DeviceBadge` `count` optional: omitted shows freshness dot only) |
| layout | `web/src/routes/+layout.svelte` | restore session on load, redirect to `/login`, wrap pages in `Shell` |
| `ssr = false`, `prerender = false` | `web/src/routes/+layout.ts` | SPA mode |
| root | `web/src/routes/+page.ts` | redirect to `/users` |
| login page | `web/src/routes/login/+page.svelte` | username/password form; special message for `ad_unreachable` |
| users list | `web/src/routes/users/+page.svelte` | list, search, select, invite |
| orgs list | `web/src/routes/orgs/+page.svelte` | org tree with search; admin creates an org in a dialog |
| org detail | `web/src/routes/orgs/[code]/+page.svelte` | org members; admin renames/re-parents, deletes, moves users in (`SearchPicker`) |
| new user | `web/src/routes/users/new/+page.svelte` | create user |
| user detail | `web/src/routes/users/[id]/+page.svelte` | view, edit, invite, delete |
| devices list | `web/src/routes/devices/+page.svelte` | device list, search, freshness badge per device |
| device detail | `web/src/routes/devices/[id]/+page.svelte` | one device: mirror data, live info and profiles, `DeviceApps`, `DeviceLocation` (when `canViewLocation`) |
| apps list | `web/src/routes/apps/+page.svelte` | installed apps across in-scope devices grouped by category (name, device count), search, category filter, show-system toggle |
| app detail | `web/src/routes/apps/[package]/+page.svelte` | one app: devices with it (avatar select, freshness, version, managed), write users uninstall from the selected devices behind a confirm dialog with per-device results; admins assign the category |
| groups list | `web/src/routes/groups/+page.svelte` | group list (create for admins; delete lives on the group detail screen) |
| group detail | `web/src/routes/groups/[id]/+page.svelte` | Members / Apps / Profiles counts summary; members with avatar select and "Remove from group (N)"; Add members; Delete group (admin) |
| account page | `web/src/routes/account/+page.svelte` | edit own display name and change password (local accounts; AD accounts see a directory-managed notice); linked from the `Shell` account menu (Edit account, Sign out) |
| admin page | `web/src/routes/admin/+page.svelte` | app accounts and audit log |
| setup page | `web/src/routes/setup/+page.svelte` | first-run wizard: code (prefilled from the `#code=` fragment by the desktop app, then stripped with `history.replaceState`), admin, Knox (skippable), done |
| Knox settings | `web/src/routes/admin/settings/knox/+page.svelte` | Knox connection settings (uses `KnoxForm`) |
| AD settings | `web/src/routes/admin/settings/ad/+page.svelte` | AD settings, group mappings, test sign-in |
| general settings | `web/src/routes/admin/settings/general/+page.svelte` | installation name, sync interval, update-check toggle with status and "Check now", show-map toggle with privacy note, app refresh interval (1-168 h) and daily app-report toggle |
| app categories | `web/src/routes/admin/settings/apps/+page.svelte` | admin: add, rename and delete categories (delete asks to confirm; mappings go with it); linked from the admin page |
| styles | `web/src/app.css` | Tailwind v4 + daisyUI themes `fclight`/`fcdark`, Figtree via @fontsource; `row-hover` for clickable rows |
| `app.html`, `app.d.ts` | `web/src/` | SvelteKit template and ambient types |
| kit/vite config | `web/vite.config.ts` | sveltekit plugin options, adapter-static, `/api` dev proxy |
| `#lib/*`, `#components/*` aliases | `web/package.json` | `imports` map to `./src/lib/*.ts` and `./src/lib/components/*` |
