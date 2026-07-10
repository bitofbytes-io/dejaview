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

Required secrets support corresponding `*_FILE` variables and default Docker secret paths under `/run/secrets/dejaview_*`.

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
