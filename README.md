# Dejaview

Dejaview is a Go web app for tracking and managing movie entries. It uses server-side rendering and a PostgreSQL-backed data model with structured migrations.

## Tech Stack

- Go (application code)
- Templ (HTML templates)
- Tailwind CSS (styles)
- PostgreSQL (database)
- Goose (database migrations)

## Project Structure

- `cmd/dejaview/main.go` is the application entry point.
- `internal/` contains handlers, middleware, repositories, models, server wiring, and config.
- `internal/ui/` contains Templ templates grouped by purpose.
- `tailwind/` is the source CSS; `static/` holds compiled assets.
- `migrations/` stores ordered SQL migrations.

## Development

In a fresh clone, generate the Templ files before running dependency or build commands. The generated `*_templ.go` files are not committed.

Generate templates and CSS, then run the app:

```
make run
```

If you only need to prepare generated files first:

```
make templ
```

Build the production binary:

```
make build
```

Run tests:

```
make test
```

## Database

Migrations are managed with Goose:

```
make migrate
make migrate-down
make migrate-status
```

## Configuration

Local configuration lives in `local.mk` (see `local.mk.example`). Typical values include:

- `DATABASE_URL`
- `API_TOKEN`
- `TMDB_API_KEY`
