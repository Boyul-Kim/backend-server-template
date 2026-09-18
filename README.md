# api-template

A starting point for Go backend APIs built on [PocketBase](https://pocketbase.io) v0.40,
used as a Go framework rather than a prebuilt binary. You get PocketBase's REST API,
auth (email/password, OAuth2, OTP, MFA), realtime, file storage, admin dashboard and
backups, plus the pieces a production service needs on top of it.

**What the template adds to PocketBase**

- Config from environment variables, validated at startup. Production refuses to start
  with a missing encryption key, a missing `APP_URL` or wildcard CORS.
- Environment variables are the source of truth for settings (SMTP, S3, rate limits,
  backups, trusted proxy), so each deploy is reproducible.
- A superuser is created from env on first start, so there's no browser installer step.
- Schema is kept as code in Go migrations. In dev the dashboard writes them for you; in
  prod the schema controls are hidden.
- Versioned custom routes under `/api/v1`, with a readiness probe and a version endpoint.
- Request ID propagation (`X-Request-Id`) into PocketBase's request logs, and HSTS.
- Integration tests that run against a real, migrated PocketBase instance.
- A distroless, non-root, static Docker image with a built-in healthcheck. Plus CI
  (tidy, vet, race tests, golangci-lint, image build).

## Quick start

Requires Go 1.27+ (PocketBase v0.40 requires it).

```sh
cp .env.example .env
make run                # http://127.0.0.1:8090
```

- Dashboard: http://127.0.0.1:8090/_/ (log in with `SUPERUSER_EMAIL` / `SUPERUSER_PASSWORD` from `.env`)
- REST API: `/api/collections/{collection}/records`, [API docs](https://pocketbase.io/docs/api-records/)
- Custom API: `/api/v1/*`

`make help` lists all targets. Without `make`, run `go run ./cmd/server serve`.

### Starting a new service from this template

```sh
cp -r api-template billing-api && cd billing-api
git init
./scripts/rename-module.sh github.com/acme/billing-api
```

Then delete the example `notes` domain (see [Adding a feature](#adding-a-feature)) and
build your own.

## Project layout

```
cmd/server/            entrypoint: load config, build app, start
internal/
  config/              env -> Config, with validation (fail fast)
  app/                 wiring: PocketBase instance, env->settings sync,
                       superuser bootstrap, serve defaults, healthcheck
  api/                 custom HTTP routes (/api/v1/...)
  hooks/               record/model lifecycle hooks (business rules)
  middleware/          global middleware (request id, HSTS)
  testutil/            helpers for integration tests
  version/             build metadata injected via -ldflags
migrations/            Go migrations: collections, indexes, rules, seed data
```

## Adding a feature

When you add a feature, choose the layer that fits. Stop at the first one that does the job.

1. **Collection + API rules (migration).** For plain CRUD you don't need Go code:
   PocketBase serves `/api/collections/<name>/records` with filtering, sorting,
   pagination, expansion and realtime. Access control goes in the
   [API rules](https://pocketbase.io/docs/api-rules-and-filters/).
   - In dev: create the collection in the dashboard. Automigrate writes a Go file to
     `migrations/`. Commit it.
   - Or write one by hand: `make migration name=create_posts`
     (see [migrations/1789718400_create_notes.go](migrations/1789718400_create_notes.go)).
2. **Hooks** ([internal/hooks](internal/hooks/hooks.go)) for logic that must run on every
   write, whatever the entry point: defaults, derived fields, invariants, side effects.
   `OnRecordValidate` / `OnRecordCreate` run for every save. `On*Request` hooks run only
   for the REST API.
3. **Custom routes** ([internal/api](internal/api/api.go)) for anything that isn't CRUD on a
   single collection: aggregates, multi-record transactions, integrations, webhooks. The
   `notes` examples show auth (`apis.RequireAuth`), body binding, validation with
   `github.com/pocketbase/ozzo-validation/v4` (field errors serialize into the standard
   error response), and `RunInTransaction`.
4. **Background jobs** via `app.Cron().MustAdd("name", "0 * * * *", fn)`, registered in
   `app.Register`.

To remove the example domain, delete `internal/api/notes.go`, the notes hooks, the notes
routes in `api.go`, the notes migration, and their tests.

> Note: collection API rules run **before** `OnRecordCreateRequest` hooks. A rule can't
> depend on a value that a request hook fills in. See the `notes` create rule.

## Configuration

All configuration comes from environment variables. The annotated list is in
[.env.example](.env.example). `make` targets load `.env` automatically.

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `development` | `production` enables rate limits and hidden schema controls, turns off automigrate, and enforces the rows marked *prod* |
| `HTTP_ADDR` | `127.0.0.1:8090` | Docker image sets `0.0.0.0:8090` |
| `CORS_ORIGINS` | `*` (PocketBase) | *prod*: explicit list required |
| `PB_ENCRYPTION_KEY` | — | *prod*: 32 chars, encrypts stored settings. Back it up. |
| `APP_URL` | — | *prod*: public base URL |
| `PB_DATA_DIR` | `./pb_data` | SQLite DBs, uploads, backups |
| `SUPERUSER_EMAIL` / `SUPERUSER_PASSWORD` | — | created once if no superuser exists |
| `SMTP_*`, `S3_*`, `BACKUPS_*` | disabled | feature turns on when `SMTP_HOST` / `S3_BUCKET` / `BACKUPS_CRON` is set |
| `TRUSTED_PROXY_HEADERS` | — | e.g. `X-Forwarded-For` behind a load balancer |
| `RATE_LIMIT_ENABLED` / `HIDE_CONTROLS` | `true` in prod | |

Settings-backed variables are applied on every start and override values saved in the
dashboard. Unset variables leave the stored value alone.

CLI flags still work and take precedence, e.g. `./server serve --http=:9000`. Every
PocketBase command is available: `./server superuser upsert ...`, `./server migrate ...`,
`./server --help`.

## Endpoints

| Route | Purpose |
|---|---|
| `GET /api/health` | liveness (built into PocketBase) |
| `GET /api/v1/ready` | readiness: the DB answers queries |
| `GET /api/v1/version` | build version and commit |
| `/api/collections/...`, `/api/realtime`, `/api/files/...`, `/api/batch` | [PocketBase Web API](https://pocketbase.io/docs/api-records/) |
| `GET /api/v1/notes/stats`, `POST /api/v1/notes/import` | example custom routes (auth required) |
| `/_/` | admin dashboard |

## Testing

```sh
make test     # go test -race ./...
```

Tests in [internal/app/app_test.go](internal/app/app_test.go) go through the full
HTTP stack. Each scenario gets a fresh temporary PocketBase with all migrations applied
and the same wiring as production. Copy the `run(...)` pattern for new endpoints and
collection rules.

## Deployment

```sh
make docker-build                  # or: docker build -t my-api .
docker compose up --build          # runs the prod image locally using .env
```

The image is `distroless/static:nonroot` (~40 MB). It runs as UID 65532 with data in the
`/pb_data` volume, and `HEALTHCHECK` runs `/app/server healthcheck`.

**Production checklist**

- [ ] Persist `/pb_data` on a durable volume, and back up `PB_ENCRYPTION_KEY` separately.
- [ ] Put a TLS-terminating reverse proxy in front (Caddy, nginx, a cloud LB). Set
      `TRUSTED_PROXY_HEADERS` so rate limits and logs see real client IPs.
- [ ] Configure SMTP. Auth emails (verification, password reset, OTP) need it.
- [ ] Set `BACKUPS_CRON` (optionally with S3 backups via the dashboard), or stream the
      DB with [Litestream](https://litestream.io).
- [ ] Use S3 for uploads if the host's disk is ephemeral.

**Scaling model.** PocketBase is backed by embedded SQLite. Run **one instance per
database** and scale vertically. A single node handles a lot of traffic, but you can't
put several replicas behind a load balancer. Deploys therefore have a few seconds of
downtime on restart. That rules out the template for services that need multi-node
active-active writes; choose a Postgres-backed stack for those.

## Upgrading PocketBase

```sh
go get github.com/pocketbase/pocketbase@latest && go mod tidy && make test
```

Read the [changelog](https://github.com/pocketbase/pocketbase/blob/master/CHANGELOG.md)
first. PocketBase is pre-1.0, and minor versions can include breaking changes.
