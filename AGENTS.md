# Agent Guidance

- Edit `.templ` sources and `tailwind/styles.css`; never edit generated `*_templ.go` or `static/styles.css` files directly.
- Generate Templ and CSS before builds or tests in a fresh clone because generated artifacts are intentionally untracked.
- Keep numbered migrations forward-compatible and preserve the existing `ON DELETE SET NULL` behavior for `entries.picked_by_person_id`.
- Use Conventional Commits, prefix maintenance branches with `chore/`, and assign pull requests to yourself.
- Use `make run` for local development; it runs `templ` and `tail-prod` before `go run ./cmd/dejaview`.
- Database helpers are `make migrate`, `make migrate-status`, and `make migrate-down`; they require `DATABASE_URL` to be configured locally.
- Run `make test` and verify affected rendered routes for handler or template changes.
