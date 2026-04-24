# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build, Test, and Development Commands

```bash
make run          # Generate Templ + Tailwind, then run the app
make build        # Generate assets and build production binary (bin/dejaview)
make test         # Run all Go tests (go test -v ./...)
make templ        # Generate Go code from Templ templates
make templ-watch  # Watch Templ files and regenerate on change
make tail-watch   # Build Tailwind CSS in watch mode
make tail-prod    # Build minified Tailwind output
make migrate      # Apply database migrations via Goose
make migrate-down # Roll back last migration
```

## Architecture Overview

Dejaview is a Go web app for tracking family movie watching. It uses server-side rendering with HTMX for interactivity.

**Tech stack:** Go 1.25.5, chi/v5 router, Templ templates, Tailwind CSS, PostgreSQL with pgx/v5, Goose migrations, HTMX

**Key directories:**
- `cmd/dejaview/main.go` - Application entry point
- `internal/handler/` - HTTP request handlers
- `internal/repository/` - Database access layer (pgx queries)
- `internal/model/` - Data structures
- `internal/ui/` - Templ templates organized as:
  - `layout/` - Base HTML layout
  - `pages/` - Full page templates (dashboard, stats, login, movie_detail)
  - `components/` - Reusable UI elements
  - `partials/` - HTMX partial templates for dynamic updates
- `internal/tmdb/` - TMDB API client for movie search/details
- `migrations/` - SQL migrations (numbered, snake_case)
- `static/` - Compiled assets (styles.css, htmx.min.js, dragdrop.js, icons/)
- `tailwind/` - Tailwind CSS source

**Request flow:** Routes defined in `internal/server/server.go` use chi middleware (RequestID, RealIP, Logger, Recoverer). Auth middleware validates Bearer token or session cookie.

**Authentication:** Single shared API token. Browser uses cookie (`dejaview_session`), programmatic clients use `Authorization: Bearer <token>`.

## Configuration

Local config in `local.mk` (gitignored). Required variables:
- `DATABASE_URL` - PostgreSQL connection string
- `API_TOKEN` - Authentication token
- `TMDB_API_KEY` - The Movie Database API key

Optional: `PORT` (default 4600), `LOG_LEVEL`, `SECURE_COOKIES` (false for local HTTP dev)

**Important:** Avoid inline comments after `export` lines in `local.mk`; trailing spaces break token matching.

## Commit Conventions

Use conventional commits with branch prefixes:
- `feat:` / `feature/<name>` - New features
- `fix:` / `bugfix/<name>` - Bug fixes
- `chore:` / `chore/<name>` - Maintenance tasks

When creating PRs, assign to self with `--assignee @me`.

## Asset Generation Notes

- `*_templ.go` files are generated from `.templ` files (gitignored)
- `static/styles.css` is generated from Tailwind (gitignored)
- In a fresh clone, generate Templ output before dependency-update, build, or test work if the generated files are missing
- Both must be regenerated before building; CI handles this automatically
