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

### People

Migration 003 seeds four example people (D, J, C and A). On a new install, replace them with everyone who picks and rates movies, each with a unique one-letter initial, before anyone picks or rates a movie:

```bash
psql "$DATABASE_URL" -c "DELETE FROM persons" -c "INSERT INTO persons (initial, name) VALUES ('A', 'Alex'), ('B', 'Blake')"
```

The `DELETE` fails once someone has ratings, so it cannot remove anyone's scores; rename a person instead with `UPDATE persons SET name = 'Alex' WHERE initial = 'A'`. A movie counts as fully rated in the Trophy Room once every person has rated it.

## Run with Docker

```bash
docker run --rm --name dejaview --network dejaview \
  --env-file dejaview.env \
  -p 4600:4600 \
  dejaview:local
```

Open <http://localhost:4600> and sign in with the value configured as `API_TOKEN`. The health endpoint is <http://localhost:4600/health>.

For production, use HTTPS, set `SECURE_COOKIES=true`, restrict the TMDB key where supported, and load credentials through a secret manager.

## Development

```bash
cp local.mk.example local.mk
make run
make test
```

Database helpers are available as `make migrate`, `make migrate-status`, and `make migrate-down` when `DATABASE_URL` is configured.

## License

Dejaview is available under the [MIT License](LICENSE).
