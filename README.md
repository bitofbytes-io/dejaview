# Dejaview

Dejaview is a self-hosted movie tracker for maintaining a watch list, recording who selected each movie, and collecting ratings. It is a server-rendered Go application backed by PostgreSQL and enriched with data from TMDB.

## Requirements

- Docker 24+
- PostgreSQL 15+
- A [TMDB API Read Access Token](https://developer.themoviedb.org/docs/authentication-application)
- Go 1.26, [Templ](https://templ.guide/), the [Tailwind CSS CLI](https://github.com/tailwindlabs/tailwindcss/releases), and Goose when building from a fresh clone

Generated Templ and CSS files are not committed, so prepare them before building the image:

```bash
go install github.com/a-h/templ/cmd/templ@v0.3.1020
make templ tail-prod
docker build -t dejaview:local .
```

## Configure the application

Generate a strong token for login and session signing:

```bash
openssl rand -base64 32
```

Create an untracked `dejaview.env` file:

```dotenv
DATABASE_URL=postgres://dejaview:change-me@db:5432/dejaview?sslmode=disable
API_TOKEN=replace-with-generated-token
TMDB_READ_ACCESS_TOKEN=replace-with-your-tmdb-read-access-token
PORT=4600
SECURE_COOKIES=false
LOG_LEVEL=info
```

Do not commit this file.

| Setting | Required | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `API_TOKEN` | Yes | Shared login credential and session-signing key |
| `TMDB_READ_ACCESS_TOKEN` | Yes | TMDB API Read Access Token for movie search and metadata (Bearer authentication) |
| `PORT` | No | HTTP port; defaults to `4600` |
| `SECURE_COOKIES` | No | Set `false` for local HTTP; defaults to `true` |
| `LOG_LEVEL` | No | Application log level; defaults to `info` |
| `TRUSTED_PROXY_CIDRS` | Behind a reverse proxy | Comma-separated CIDRs or IPs whose `X-Forwarded-For` header is trusted; defaults to empty, which ignores forwarded headers and disables the per-IP login limit |
| `SESSION_EPOCH` | No | Non-negative integer; raise it and redeploy to sign out every browser without changing `API_TOKEN`; defaults to `0` |
| `METRICS_ADDR` | No | Listen address for the internal Prometheus metrics listener; defaults to `:9464`; set `off` to disable it |

Required secrets support corresponding `*_FILE` variables and default Docker secret paths under `/run/secrets/dejaview_*`.

TMDB uses its **API Read Access Token**, not the v3 API key. Set `TMDB_READ_ACCESS_TOKEN`, or `TMDB_READ_ACCESS_TOKEN_FILE` to a mounted file; the default file is `/run/secrets/dejaview_tmdb_read_access_token`. A nonempty environment value takes precedence over the explicit file, which takes precedence over the default path. The old `TMDB_API_KEY` and `dejaview_tmdb_api_key` secret are not fallback credentials.

Before deploying this change, an operator must securely provision the actual read-access token and authorize its persistent mount into the application service at that path (or configure the new `_FILE` path). The ordinary image deployment hook does not add secret mounts. Deploy the new image only after the token and mount are ready; verify movie search and adding a movie after the coordinated release. Preserve the old image and its API-key secret for rollback until the new release is verified. Never put token values in Git, issue comments, or command-line arguments.

TMDB requests keep credentials out of URLs and do not follow redirects, including redirects to the same host.

Failed logins are limited to 10 per client IP and 50 from all clients together in a 15-minute window. Further attempts get HTTP 429 with `Retry-After` until the window ends, and a successful login clears the all-clients count. Counters are in memory per replica and reset on restart. The per-IP limit applies only when `TRUSTED_PROXY_CIDRS` is set: a peer outside those CIDRs is a direct client keyed by its TCP address, and a peer inside them is keyed by the rightmost `X-Forwarded-For` address that is not a trusted proxy. Otherwise only the all-clients limit applies, which anyone can use up to block new logins for 15 minutes; browsers that are already signed in are unaffected. Behind Traefik on a Docker Swarm overlay network, set `TRUSTED_PROXY_CIDRS` to that network's subnet, for example the output of `docker network inspect proxy --format '{{range .IPAM.Config}}{{.Subnet}} {{end}}'`.

Sessions last 90 days. Cookies issued before `SESSION_EPOCH` existed count as epoch 0, so they stay valid until the epoch is raised.

## Database and migrations

Start a local database:

```bash
docker network create dejaview

docker run -d --name db --network dejaview \
  -e POSTGRES_DB=dejaview \
  -e POSTGRES_USER=dejaview \
  -e POSTGRES_PASSWORD=change-me \
  -p 5432:5432 \
  -v dejaview-postgres:/var/lib/postgresql/data \
  postgres:17
```

Apply the schema before starting the application. With Goose installed:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL='postgres://dejaview:change-me@localhost:5432/dejaview?sslmode=disable'
goose -dir migrations postgres "$DATABASE_URL" up
```

The image also ships `/app/dejaview-migrate`, which applies the same migrations (embedded in the binary) and records them in Goose's `goose_db_version` table. It reads the database URL the way the app does: `DATABASE_URL`, then `DATABASE_URL_FILE`, then the `/run/secrets/dejaview_database_url` Swarm secret. The home_swarm deploy hook runs it as a one-off job before rolling out each new image, so production migrations apply on deploy.

### People

Manage people in the app at `/people` (the **People** link in the header; sign in first). Add everyone who picks and rates movies, give each a unique one-letter initial and one of five colours (cyan, rose, amber, mint, violet), and rename or recolour them later. The initial is fixed once a person is added. A person who has ratings or has picked a movie can't be removed, so no scores or picks are lost; rename them instead.

Migration 003 seeds four example people (D, J, C and A) with the colours cyan, rose, amber and violet. On a new install, add your own people on the People page, then remove the examples before anyone picks or rates a movie. A movie counts in the Trophy Room (rating trophies, Genre Spotlight ratings, Family Top Five and the family-rated total) once all but one person has rated it, and always at least one: 3 of 4 people, or 1 of 2. Its score is the average of the ratings it has.

Deleting or moving the last movie out of a group compacts later group numbers in the same transaction. Dashboard add requests carry `group_version`; an invalidated selection returns HTTP 409 and refreshes the groups. Numeric callers may omit the version to target the current numbering at request start; repository writes also reject numbering changes while waiting for the write lock. Reorders identify every entry by UUID and reject a changed group. The historical first group retains its eight-pick total until it disappears; a later group renumbered to 1 keeps the normal nine-pick total. Migration 015 persists this state independently of group numbers.

The dashboard nudges people to rate: each poster shows a dashed initial for everyone who hasn't rated it yet, a **Waiting on** line above the groups counts each person's unrated movies, and the **Needs rating from** dropdown (or `/?needs=<initial>`) shows only one person's unrated movies. The choice is remembered in a `dv_me` cookie, which is just a default for the filter, not a login; choose **Anyone** (`/?needs=`) to see every movie again.

No configuration is needed beyond running migration 013, which adds `persons.color`.

The Trophy Room's Genre Spotlight reads the genres TMDB returned when each movie was added (saved in `movies.metadata_json`), so it needs no extra configuration. Movies without saved genres are skipped. A genre needs two family-rated movies to be the family favorite, and the Genre Loyalist trophy needs three genre-tagged picks.

The Trophy Room's Taste Match compares every pair of people over the movies both have rated, family-rated or not. Each cell shows how many points apart their scores are on average (±), or, once the pair shares five movies, the correlation of their scores (−1 to 1). Most Divisive Movie is the family-rated movie with the widest gap between its lowest and highest score. The Contrarian is whoever's scores sit furthest from the average of everyone else's on the same family-rated movies, after rating at least three of them. It's all computed from `ratings`, with no extra configuration.

## Run with Docker

```bash
docker run --rm --name dejaview --network dejaview \
  --env-file dejaview.env \
  -p 4600:4600 \
  dejaview:local
```

Open <http://localhost:4600> and sign in with the value configured as `API_TOKEN`. Two endpoints need no sign-in. <http://localhost:4600/health> returns `ok` whenever the process is up. <http://localhost:4600/ready> pings the database (2 s timeout) and returns `{"status":"ok","db":"ok"}`, or HTTP 503 with `{"status":"degraded","db":"error"}` when the database is unreachable. Neither needs any configuration.

### Metrics

`METRICS_ADDR` is trimmed and `off` is case-insensitive. Other values must be a valid IP address or hostname plus a numeric port from 0 to 65535 (IPv6 addresses need brackets). Invalid syntax stops startup; validation does not resolve hostnames. Keep the metrics port private and never publish it through Docker or the public proxy.

Prometheus metrics are served at `GET /metrics` on a separate internal listener (`METRICS_ADDR`, default `:9464`), never on the public port, so a reverse proxy routing the app port can't expose them. Scrape it over a private network only, and don't publish the port. `METRICS_ADDR=off` disables the listener. If the address can't be bound, the app logs the error and keeps running without it.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `http_requests_total` | `method`, `route`, `status` | Requests handled. `route` is the router pattern (e.g. `/movies/{id}`), or `unmatched` for unknown paths; `/health`, `/ready` and `/metrics` are not counted |
| `http_request_duration_seconds` | `method`, `route`, `status` | Request latency histogram |
| `pgxpool_acquired_conns`, `pgxpool_idle_conns`, `pgxpool_constructing_conns`, `pgxpool_total_conns`, `pgxpool_max_conns` | | Database pool gauges |
| `pgxpool_acquire_count_total`, `pgxpool_acquire_duration_seconds_total`, `pgxpool_empty_acquire_count_total`, `pgxpool_empty_acquire_wait_seconds_total`, `pgxpool_canceled_acquire_count_total` | | Database pool counters; empty acquires waited for a free connection |
| `dejaview_login_failures_total` | `reason` | Failed logins: `invalid_key`, `missing_key`, `invalid_request` or `rate_limited` |
| `dejaview_entries_added_total` | `result` | Entry creation calls: `success` after confirmed commit; `failure` for known rejection (including duplicates or a group becoming invalid); `unknown` when a commit outcome cannot be confirmed. Movie metadata writes alone and requests rejected before entry creation do not count |
| `dejaview_ratings_saved_total` | `result` | Number of non-empty scores in each atomic rating batch: `success` after confirmed commit; `failure` for attempted scores when the batch is known to fail; `unknown` for attempted scores when the commit outcome cannot be confirmed. Deletions, omitted scores and empty batches do not count; saving a score again counts as another save |
| `dejaview_tmdb_lookups_total` | `operation`, `result` | Actual TMDB HTTP attempts. `operation` is `search` or `movie`; `result` is `success` after HTTP 200 and JSON decode, `failure` for transport/status/decode errors, or `not_found` for a movie-detail 404. Blank searches and cached movie reuse make no lookup and do not count |

These counters use only the fixed labels above, never titles, queries, IDs, tokens or person names. They live in the same private registry served at the configured `METRICS_ADDR` (default `:9464`, disabled with `off`); the public `/metrics` remains a 404. A confirmed commit still counts as success if response rendering or a later read fails. Cancellation or a network failure during COMMIT may leave its outcome unknown; those writes count only as `unknown`, without retrying or disabling request cancellation. They count operations for this process, not the current number of stored entries or ratings.

The Go runtime (`go_*`) and process (`process_*`) collectors are also included.

For production, use HTTPS, set `SECURE_COOKIES=true`, and load credentials through a secret manager.

All application responses set `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, and `X-Frame-Options: DENY`. When `SECURE_COOKIES=true`, responses also set `Strict-Transport-Security: max-age=31536000` (one year, without subdomains or preload). HSTS follows this deployment setting, including behind an HTTPS proxy; request forwarding headers do not enable it. Set `SECURE_COOKIES=false` for local HTTP development.

Form submissions (including login) and entry-reorder JSON accept request bodies up to 64 KiB; larger bodies receive HTTP 413 before any changes are saved. Reorder accepts a single JSON value with optional trailing whitespace. TMDB error diagnostics retain at most 4 KiB of the upstream body, followed by `[truncated]` when more data was returned.

## Development

```bash
cp local.mk.example local.mk
make run
make test
```

Database helpers are available as `make migrate`, `make migrate-status`, and `make migrate-down` when `DATABASE_URL` is configured.

CI uses golangci-lint v2.15.0 for formatting, error handling, HTTP response cleanup, static analysis, and Go vet checks. With that version installed, run the same lint gate locally after generating the untracked assets:

```bash
make templ tail-prod
golangci-lint run ./...
```

## License

Dejaview is available under the [MIT License](LICENSE).
