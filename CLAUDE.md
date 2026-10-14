# knoxmgr — standing constraints for every agent

Go module: `github.com/DonovanDiamond/knoxmgr` (repo https://github.com/DonovanDiamond/knoxmgr) — all imports use this prefix.

Web UI for managing a Samsung Knox Manage tenant (users, enrollment invitations, user groups).
Follows the house "donovan-style" stack: Go + gofiber v3 + zerolog + sqlx (pure-Go SQLite, optional
Postgres) + migrations; SvelteKit (Svelte 5 runes, TS, adapter-static) embedded in the Go binary;
Tailwind v4 + daisyUI. Single binary, CGO_ENABLED=0, linux/amd64 + windows/amd64.

## Project decisions (see QUESTIONS.md for reasoning)
- **Browsers: evergreen only.** The house Firefox ESR 115 constraint does NOT apply here.
- **Knox is the source of truth, mirrored locally** (decided 2026-10-12, reverses the earlier
  no-mirror decision). A background sync copies Knox orgs, users, groups, memberships and devices
  into the DB on a schedule (default 15 min, admin-editable) plus a "Sync now" action. Reads come
  from the mirror; writes go to Knox immediately, then update the mirror.
- Knox API spec: `docs/knox-api/km-api.yml` (OpenAPI 3, v25.07). Requests are
  `application/x-www-form-urlencoded`; responses carry `resultCode` ("0" = success).
  OAuth client_credentials at `https://<region>.manage.samsungknox.com/emm/oauth/token`,
  client_id is `{clientId}@{tenantId}`. Reuse the token until it expires.
- **Knox connection and AD settings live in the DB**, edited in admin settings screens (with
  "Test connection"), NOT in config.yaml. Secrets (Knox client secret, any AD bind password) are
  encrypted at rest with an auto-generated key file; never returned to the UI, only replaced.
- **First run:** config.yaml is optional with sensible defaults. On first start the log prints a
  one-time setup code; the browser setup page asks for it, then creates the first admin and
  collects Knox settings. Goal: a colleague can run it, connect their own tenant, and it just works.
- Scope: Knox **USER groups only** (no device groups). Org management (create/update/delete,
  move users into an org) is **admin-only**; users view orgs in their scope (decided 2026-10-14). No group rename
  endpoint exists in Knox.
- Roles: `admin` (everything) and `user`. A `user` is bound to exactly ONE Knox organization
  (orgCode) with access `read` or `write`; access covers that org **and its sub-orgs**.
  - read: view users in scope, view groups (members filtered to scope).
  - write: also create/update/purge users in scope, send enrollment invitations, add/remove
    in-scope users to/from groups. Only admins create/delete groups.
- Every mutating action is audit-logged (who, what, when, previous value).
- Purging a Knox user is irreversible: API requires an explicit confirmation field.

## Rules
- Security first: validate all input at the edge, parameterized SQL, authorization on every
  route (scope checks in one place, not scattered), no secrets/tokens in logs.
- Clean minimal code, table-driven tests, temp SQLite for repository tests.
- Run `go test ./...` and `npm run check` (in web/) after changes that could break them.
- Keep README.md, IMPLEMENTATION.md, QUESTIONS.md, ARCHITECTURE.md, CODE-BLOCKS-REFERENCE.md current.
