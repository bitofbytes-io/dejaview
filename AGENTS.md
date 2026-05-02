# Repository Guidelines

## Project Structure & Module Organization
- `cmd/dejaview/main.go` is the entry point.
- `internal/` holds app code: handlers, middleware, repositories, models, server wiring, and config.
- `internal/ui/` contains Templ templates (`components/`, `partials/`, `pages/`, `layout/`).
- `tailwind/` is the source CSS; `static/` is the compiled assets output (e.g., `static/styles.css`).
- `migrations/` stores ordered SQL migrations (e.g., `001_create_movies.sql`).

## Build, Test, and Development Commands
- `make run`: generate Templ + Tailwind, then run the app (`go run ./cmd/dejaview`).
- `make build`: generate assets and build the production binary (`bin/dejaview`).
- `make test`: run Go tests (`go test -v ./...`).
- `make docker-buildx`: build and push the multi-arch Docker image; set `REGISTRY`, `IMAGE_REPO`, `PLATFORMS`, and `TAG` as CI does.
- `make templ` / `make templ-watch`: generate Templ Go code (watch mode available).
- `make tail-prod` / `make tail-watch`: build Tailwind CSS (watch requires Tailwind CLI).
- `make migrate`, `make migrate-down`, `make migrate-status`: manage database migrations with Goose.
- In a fresh clone, run `make templ` before dependency-update, build, or test work if the generated `*_templ.go` files are missing.
- GitHub Actions on `main` generates Templ, builds Tailwind, runs `make docker-buildx`, then force-pushes to the configured local bare repo (default `/srv/git/dejaview-ci.git`) to trigger deployment.

## Coding Style & Naming Conventions
- Go code follows standard `gofmt` formatting and idiomatic package structure under `internal/`.
- Templ files use `.templ` and are grouped by purpose (`components`, `partials`, `pages`).
- Migration files use zero-padded numeric prefixes and snake_case names.
- Local configuration lives in `local.mk` (see `local.mk.example`) using `export KEY=VALUE`.

## Testing Guidelines
- Use Go’s built-in testing via `go test -v ./...`.
- Name tests with `_test.go` files and `TestXxx` functions.

## Commit & Pull Request Guidelines
- **Branch naming**: Use prefixed branch names:
  - `feature/<descriptive-name>` for new features
  - `bugfix/<descriptive-name>` for bug fixes
  - `chore/<descriptive-name>` for maintenance tasks
- **Commit messages**: Use conventional commit format with short, imperative subjects:
  - `feat: <description>` for new features
  - `fix: <description>` for bug fixes
  - `chore: <description>` for maintenance tasks (deps, config, etc.)
  - Examples: "feat: add rating validation", "fix: correct null pointer in stats", "chore: update dependencies"
- PRs should include a clear description, testing notes, and screenshots for UI changes.
- When creating PRs, assign them to yourself (`--assignee @me`).

## Security & Configuration Tips
- Keep secrets in `local.mk` (gitignored). Required values include `DATABASE_URL`, `API_TOKEN`, and `TMDB_API_KEY`.
- Avoid inline comments after `export` lines in `local.mk`; `make` keeps the trailing space, which can break token matching.
- For local HTTP dev, set `SECURE_COOKIES=false`; keep it true for production HTTPS.

## Database Notes
- `entries.picked_by_person_id` uses `ON DELETE SET NULL` (see `migrations/008_set_picked_by_person_on_delete.sql`) so deleting a person clears the picker.
