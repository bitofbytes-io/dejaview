# Dejaview

Dejaview is a self-hosted movie tracker for maintaining a watch list, recording who selected each movie, and collecting ratings. It is a server-rendered Go application backed by PostgreSQL and enriched with data from TMDB.

## Requirements

- Docker 24+
- PostgreSQL 15+
- A [TMDB API key](https://developer.themoviedb.org/docs/getting-started)
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
TMDB_API_KEY=replace-with-your-tmdb-key
PORT=4600
SECURE_COOKIES=false
LOG_LEVEL=info
```

Do not commit this file.

| Setting | Required | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `API_TOKEN` | Yes | Shared login credential and session-signing key |
| `TMDB_API_KEY` | Yes | Movie search and metadata |
| `PORT` | No | HTTP port; defaults to `4600` |
| `SECURE_COOKIES` | No | Set `false` for local HTTP; defaults to `true` |
| `LOG_LEVEL` | No | Application log level; defaults to `info` |
| `TRUSTED_PROXY_CIDRS` | Behind a reverse proxy | Comma-separated CIDRs or IPs whose `X-Forwarded-For` header is trusted; defaults to empty, which ignores forwarded headers and disables the per-IP login limit |
| `SESSION_EPOCH` | No | Non-negative integer; raise it and redeploy to sign out every browser without changing `API_TOKEN`; defaults to `0` |
| `METRICS_ADDR` | No | Listen address for the internal Prometheus metrics listener; defaults to `:9464`; set `off` to disable it |

Required secrets support corresponding `*_FILE` variables and default Docker secret paths under `/run/secrets/dejaview_*`.

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

Prometheus metrics are served at `GET /metrics` on a separate internal listener (`METRICS_ADDR`, default `:9464`), never on the public port, so a reverse proxy routing the app port can't expose them. Scrape it over a private network only, and don't publish the port. `METRICS_ADDR=off` disables the listener. If the address can't be bound, the app logs the error and keeps running without it.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `http_requests_total` | `method`, `route`, `status` | Requests handled. `route` is the router pattern (e.g. `/movies/{id}`), or `unmatched` for unknown paths; `/health`, `/ready` and `/metrics` are not counted |
| `http_request_duration_seconds` | `method`, `route`, `status` | Request latency histogram |
| `pgxpool_acquired_conns`, `pgxpool_idle_conns`, `pgxpool_constructing_conns`, `pgxpool_total_conns`, `pgxpool_max_conns` | | Database pool gauges |
| `pgxpool_acquire_count_total`, `pgxpool_acquire_duration_seconds_total`, `pgxpool_empty_acquire_count_total`, `pgxpool_empty_acquire_wait_seconds_total`, `pgxpool_canceled_acquire_count_total` | | Database pool counters; empty acquires waited for a free connection |
| `dejaview_login_failures_total` | `reason` | Failed logins: `invalid_key`, `missing_key`, `invalid_request` or `rate_limited` |

The Go runtime (`go_*`) and process (`process_*`) collectors are also included.

For production, use HTTPS, set `SECURE_COOKIES=true`, restrict the TMDB key where supported, and load credentials through a secret manager.

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
