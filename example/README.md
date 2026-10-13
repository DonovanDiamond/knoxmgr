# knoxmgr example

knoxmgr plus the fake Knox server (`knoxmock`), from the images `knoxmgr` and `knoxmgr-mock`, for local testing over plain HTTP.

```
make image   # build knoxmgr:latest and knoxmgr-mock:latest (needs Docker; builds the web UI inside)
make up      # start both containers
make logs    # find the line "first-run setup code" and copy the value of its setupCode field
open http://localhost:8088
```

The first visit opens the setup page:

1. Enter the setup code from the log (`XXXX-XXXX-XXXX`).
2. Create the admin account (username, password of 8 to 72 bytes).
3. Connect Knox: region "Custom URL", base URL `http://knoxmock:9090`, client ID `demo`, client secret
   `demo-secret`, any tenant ID, tick "allow insecure HTTP". Press Test, then Save.

The setup code lives in memory only. If you restart before creating the admin, a new code is logged.
Volumes from before the setup flow (milestone 7a) hold old settings and keys: run `make reset` once.

Other targets: `make logs`, `make down` (keeps data), `make reset` (deletes the data volume),
`make admin ADMIN_USER=name` (extra local admin; prompts for a password, or set `KNOXMGR_ADMIN_PASSWORD`).
All targets run Docker via `sudo`; use `make DOCKER=docker ...` when your user is in the docker group.

`config.yaml` only sets `secureCookies: false` because this stack is plain HTTP; behind a TLS proxy remove it. "Allow insecure HTTP" on the Knox settings is for the mock only.
The database, JWT keys and secrets key live in the `data` volume, in `/var/lib/knoxmgr/data`.

## Packaging notes

- Root `Dockerfile` builds the web UI (node), then static `knoxmgr` and `knoxmock` binaries (golang:1.26,
  `CGO_ENABLED=0`), into `distroless/static:nonroot`. Two targets: `knoxmgr` (entrypoint `knoxmgr -config /etc/knoxmgr/config.yaml`,
  the default) and `knoxmock` (entrypoint `knoxmock`).
- Root `Makefile`: `make help`, `image`, `build` (to `bin/`), `build-windows`, `test`, `clean`.
