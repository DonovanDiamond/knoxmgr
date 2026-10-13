# knoxmgr

A web UI for administering a Samsung Knox Manage tenant: users, enrollment invitations and user
groups, with devices shown read-only. Single binary, web UI embedded.

## Quick start

**Docker** (`make image` builds the `knoxmgr` image yourself instead):

```yaml
services:
  knoxmgr:
    image: ghcr.io/donovandiamond/knoxmgr:latest
    ports:
      - "8080:8080"
    volumes:
      - data:/var/lib/knoxmgr
volumes:
  data:
```

Run `docker compose up -d`.

**Binary:** run `./knoxmgr`. Data is kept in `./data`.

Sign-in cookies are `Secure`, so away from localhost serve knoxmgr over HTTPS (or set `secureCookies: false` in `config.yaml` for testing).

Then:

1. Open http://localhost:8080.
2. Copy the one-time setup code from the log (the line `first-run setup code`; with Docker,
   `docker compose logs knoxmgr`) into the setup page.
3. Create the first admin account.
4. Enter your Knox details: region, client ID, tenant ID and client secret. Create these in the
   Knox Manage console under Settings > API / Open API (see the Samsung Knox docs; the exact menu
   may differ).
5. Click **Test**, then **Save**. Done.

knoxmgr speaks plain HTTP. For real use put a TLS reverse proxy in front. For plain HTTP only, add
`secureCookies: false` to a `config.yaml` (otherwise sign-in does not stick).

## Updating

Pull the new image and recreate the container, or replace the binary. The data directory is kept.

The version is shown in the UI (set at build time from the git tag). Admins see an "Update
available" notice when a newer release exists: the server checks GitHub releases every 24 hours.
For air-gapped installs turn it off under Admin > Settings > General > **Check for updates**.

## Configuration

| Flag | Environment | Meaning |
|---|---|---|
| `-data <dir>` | `KNOXMGR_DATA` | data directory, default `./data` (`/var/lib/knoxmgr/data` in the image) |
| `-listen <host:port>` | `KNOXMGR_LISTEN` | listen address, default `:8080` |
| `-config <path>` | | optional `config.yaml`, default `config.yaml` |
| `-create-admin <username>` | `KNOXMGR_ADMIN_PASSWORD` | recovery: create a local admin, then exit |

`config.yaml` is optional and holds only what is needed before the database opens: `listenAddr`,
`timezone`, `secureCookies`, `trustedProxies`, `database` (`driver`, `dsn`) and `jwt` (`accessTTL`,
`refreshTTL`). Knox, Active Directory and everything else are set in the browser under
Admin > Settings. Details: [IMPLEMENTATION.md](IMPLEMENTATION.md).

## Demo with the mock

`example/` runs knoxmgr against `knoxmock`, a fake Knox server, so no tenant is needed:

```
cd example
make image && make up
```

Open http://localhost:8088 and connect Knox to `http://knoxmock:9090`, client ID `demo`, secret
`demo-secret` (see `example/README.md`). `make down` stops it, `make reset` also deletes the data.

## More documents

[IMPLEMENTATION.md](IMPLEMENTATION.md) (how it is built) ·
[QUESTIONS.md](QUESTIONS.md) (why it is built that way) ·
[ARCHITECTURE.md](ARCHITECTURE.md) (module map, flows) ·
[CODE-BLOCKS-REFERENCE.md](CODE-BLOCKS-REFERENCE.md) (block to file index).

## How it works

Knox Manage stays the source of truth, but knoxmgr keeps a local mirror of Knox orgs, users, groups,
memberships and devices. A sync runs every 15 minutes by default (a general setting) and an admin can
run one at any time with **Sync now** under `/admin/settings/general`. Screens read from the mirror;
every change is sent to Knox first. knoxmgr also stores its own application accounts, refresh tokens,
settings and an audit log. It does not manage device groups or the organization tree.

The device screen also shows live details, installed apps and (admins and write users only) location
from Knox, with the last answer kept for when Knox is unreachable. Location is drawn on an embedded
map (Leaflet, tiles from OpenStreetMap) with a coordinates table and an OpenStreetMap link. Admins can
turn the map off under Admin > Settings > General > **Show map**: the viewer's browser loads tiles
from `tile.openstreetmap.org`, which sees its IP address and the approximate area viewed.

## Vocabulary

- **Tenant** — one Knox Manage customer account, reached with one set of API credentials.
- **Organization (org)** — a node in the Knox organization tree, identified by its `orgCode`.
  Orgs nest; an org's **sub-orgs** are all orgs beneath it at any depth.
- **App user** — someone who signs in to knoxmgr. Not the same thing as a Knox user.
- **Knox user** — a user record in the Knox tenant, the people whose devices are enrolled.
- **Enrolled device** — a Knox device whose status is `A`, `B`, `BS`, `BA` or `BL`.
- **Freshness** — how recently a device (or a user's enrolled devices) connected. Values: `fresh` (last connection 24h ago or less), `stale` (over 24h), `old` (over 7 days). On a user, `none` means no enrolled device, and `old` also covers an enrolled device that never connected. On a single device, `never` means enrolled but never connected, and `not_enrolled` means the device is not enrolled so is not graded. A user's grade comes from their stalest enrolled device. Badge colours: `fresh` green, `stale` yellow, `old` red, `never` and `not_enrolled` grey, `none` the default badge.
- **Enrollment invitation** — a message sent to a Knox user so they can enroll a device.
- **User group** — a Knox group of Knox users. Device groups are out of scope.
- **Scope** — the set of orgs an app user may act on: their bound org plus its sub-orgs.

## Roles and scope

Every app user has one of two roles.

**admin** can do everything, in every org, including creating and deleting user groups.

**user** is bound to exactly one Knox org and has an access level of `read` or `write`. Scope is
that org and all of its sub-orgs, never its parent or siblings.

| Action | admin | user, read | user, write |
|---|---|---|---|
| View Knox users | all orgs | in scope | in scope |
| Create, update, purge Knox users | yes | no | in scope |
| View devices | all, incl. unassigned | owner in scope | owner in scope |
| View device location (every view audited) | all | no | in scope |
| Send enrollment invitations | yes | no | in scope |
| View user groups | all, all members | all groups, members filtered to scope | same as read |
| Add or remove members of a group | any user | no | in-scope users only |
| Create or delete user groups | yes | no | no |

### Worked examples

Org tree: `HQ` contains `Sales` and `Support`; `Sales` contains `Sales-North`.

- Alice is a `user` bound to `Sales` with `write`. She can see and edit users in `Sales` and
  `Sales-North`. She cannot see users in `HQ` or `Support`, and an attempt to edit one is rejected,
  not silently ignored.
- Bob is a `user` bound to `Sales` with `read`. He sees the same users as Alice but every change
  button is absent and every change request is rejected.
- Bob is bound to one org only. Seeing `Sales` and `Support` together means two accounts or an
  admin; it is not a configuration option.
- Group `Field Staff` has members from `Sales-North`, `Support` and `HQ`. Alice sees the group
  (groups are visible to users of any org), but its member list shows only the `Sales-North`
  member. The group list does show Knox's total member count.
- Alice adds a `Sales-North` user to `Field Staff`: allowed. She tries to add a `Support` user:
  rejected, because that user is outside her scope. She can remove only in-scope members.
- Alice tries to create or delete `Field Staff`: rejected. Only admins create or delete groups.
  Knox has no group-rename endpoint, so groups cannot be renamed.
- Purging a Knox user is irreversible. The request must carry an explicit confirmation field;
  without it the request is rejected.

Every mutating action is written to the audit log: who, what, when and the previous value. Viewing a device's location is audited too (`device.location.view`).

## Signing in

App users sign in either with a **local account** (username and password stored hashed in
knoxmgr) or with **Active Directory**. AD users get their role, org and access from the
configured mapping of AD groups. A local account with the same username always wins: that name
signs in locally only and never falls through to AD. Usernames are case-insensitive and stored
lower-case. If AD is configured but cannot be reached, sign-in fails with a
distinct "AD unreachable" error rather than a generic bad-password error, and local accounts keep
working.

The first admin is created on the browser setup page, which asks for a one-time code printed in the
start-up log. `knoxmgr -create-admin <username>` creates one from the command line for recovery; see
IMPLEMENTATION.md.

## Constraints

- Single binary with the web UI embedded. `CGO_ENABLED=0`; builds for linux/amd64 and
  windows/amd64.
- SQLite (pure Go) by default; PostgreSQL optional.
- knoxmgr speaks plain HTTP. TLS is terminated at a reverse proxy in front of it. The refresh
  cookie is `Secure`, so browsers only send it over HTTPS; for a plain-HTTP
  install set `secureCookies: false` in `config.yaml` (the one setting most installs need there).
- `make image` builds two images from the one Dockerfile: `knoxmgr` (the manager) and
  `knoxmgr-mock` (the fake Knox server), each tagged with the version and `latest`.
- Behind a reverse proxy, set `trustedProxies` in `config.yaml` to the proxy's IPs or CIDRs
  (default empty: no forwarding header is believed and the client IP is the socket peer). The proxy
  must set `X-Forwarded-For` and, if it rewrites `Host`, `X-Forwarded-Host` (the Origin check on
  refresh and logout compares against the host). With the list set, the client IP is the
  rightmost `X-Forwarded-For` hop that is not a trusted proxy. The login rate limit keys on it.
- Knox URLs must be `https://` unless "allow insecure HTTP" is ticked in the Knox settings, which is
  for the mock only; knoxmgr logs a warning when it is applied. It also warns when the AD URL is
  `ldap://` (AD passwords unencrypted); use `ldaps://`.
- Response headers: a `Content-Security-Policy` on the web pages, `Strict-Transport-Security`
  when `secureCookies` is on, `Cache-Control: no-store` on every `/api` response, and a
  `Permissions-Policy` that denies camera, microphone and geolocation.
- Browsers: evergreen only (current Chrome, Edge, Firefox, Safari). The house Firefox ESR 115
  limit does not apply here.
- Knox credentials, Active Directory and the installation name are settings in the database, edited by
  admins under `/admin/settings`. The Knox client secret and AD bind password are encrypted with a
  key file in the data directory (back the directory up as a whole) and are never shown again.
- Knox API reference: `docs/knox-api/km-api.yml`.
