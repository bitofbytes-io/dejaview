# Dejaview Project Context

## Project Overview
Dejaview is a movie tracking and rating application built with Go. It allows users to search for movies via the TMDB API, add them to a personal list, rate them, and view statistics. The application uses server-side rendering with Templ and enhances interactivity with HTMX.

## Tech Stack
- **Language:** Go 1.25.5
- **Web Framework:** [Chi](https://github.com/go-chi/chi)
- **Templating:** [Templ](https://templ.guide)
- **Styling:** [Tailwind CSS](https://tailwindcss.com)
- **Database:** PostgreSQL (driver: [pgx](https://github.com/jackc/pgx))
- **Migrations:** [Goose](https://github.com/pressly/goose)
- **Frontend Interactivity:** [HTMX](https://htmx.org)
- **External API:** TMDB (The Movie Database)

## Directory Structure
- `cmd/dejaview/`: Application entry point (`main.go`).
- `internal/`: Private application code.
  - `config/`: Configuration loading (Env vars, Docker secrets).
  - `handler/`: HTTP request handlers (controllers).
  - `middleware/`: HTTP middleware (Auth, Logger).
  - `model/`: Domain data structures.
  - `repository/`: Database access layer.
  - `server/`: HTTP server and router setup.
  - `tmdb/`: Client for the TMDB API.
  - `ui/`: UI components and pages using Templ.
- `migrations/`: SQL migration files.
- `static/`: Compiled static assets (CSS, JS, images).
- `tailwind/`: Tailwind CSS source files.

## Development Workflow

### Key Commands (Makefile)
- `make run`: Generate Templ files, build Tailwind, and run the application locally.
- `make build`: Build the production binary (`bin/dejaview`).
- `make test`: Run Go tests.
- `make migrate`: Apply database migrations.
- `make migrate-down`: Rollback the last migration.
- `make templ`: Generate Go code from `.templ` files.
- `make tail-watch`: Watch and rebuild Tailwind CSS changes.
- In a fresh clone, run `make templ` before dependency-update, build, or test work if generated Templ files are missing.

### Configuration
Configuration is loaded via `internal/config/config.go`. Local development secrets should be placed in `local.mk` (see `local.mk.example`).

**Required Environment Variables:**
- `DATABASE_URL`: PostgreSQL connection string.
- `API_TOKEN`: Token for application authentication.
- `TMDB_API_KEY`: API key for The Movie Database.

**Optional:**
- `PORT`: HTTP server port (default: `4600`).
- `LOG_LEVEL`: Logging level (default: `info`).
- `SECURE_COOKIES`: Set to `false` for local dev (default: `true`).

## Architecture & Conventions
- **Routing:** All routes are defined in `internal/server/server.go`.
- **Database:** Raw SQL queries are preferred within the `repository` package.
- **UI:** The UI is component-based using Templ. Pages are in `internal/ui/pages/`, and reusable components are in `internal/ui/components/`.
- **Auth:** Simple token-based authentication via middleware.
- **PRs:** Assign PRs to yourself when creating them (`--assignee @me`).
