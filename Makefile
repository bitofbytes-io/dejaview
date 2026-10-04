.DEFAULT_GOAL := help
TEMPL_VERSION := $(shell go list -m -f '{{.Version}}' github.com/a-h/templ)
TEMPL := go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)
.PHONY: help configure-image ensure-image-tag run build test docker-buildx tail-watch tail-prod migrate migrate-down migrate-status templ templ-watch

# Include local.mk for local environment variables (API keys, DATABASE_URL, etc.)
-include local.mk

configure-image:
	$(eval SHORT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null))
	$(eval REVISION ?= $(shell git rev-parse HEAD 2>/dev/null))
	$(eval TAG ?= $(if $(SHORT_SHA),$(SHORT_SHA),dev))
	$(eval VERSION ?= $(TAG))
	$(eval SOURCE_URL ?= https://github.com/bitofbytes-io/dejaview)
	@true

ensure-image-tag: configure-image
	@test -n "$(strip $(SHORT_SHA))" || (echo "Unable to determine git short SHA. Commit your work before building images." >&2; exit 1)

# Templ code generation
templ: ## Generate Go code from templ files
	@find internal -type f -name '*_templ.go' -exec sh -c 'for generated do test -f "$${generated%_templ.go}.templ" || rm -- "$$generated"; done' sh {} +
	$(TEMPL) generate

templ-watch: ## Watch templ files and regenerate on change
	$(TEMPL) generate --watch

# Local development (assumes tailwindcss binary is installed)
run: templ tail-prod ## Generate templ, build Tailwind, and run the app
	go run ./cmd/dejaview

build: configure-image templ tail-prod ## Generate templ, build Tailwind, and build production binary
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION) -X main.revision=$(REVISION)" -o bin/dejaview ./cmd/dejaview

# Tailwind (using standalone CLI binary)
tail-watch: ## Build Tailwind in watch mode (requires tailwindcss CLI)
	tailwindcss -i ./tailwind/styles.css -o ./static/styles.css --watch

tail-prod: ## Build minified Tailwind output to static/styles.css
	tailwindcss -i ./tailwind/styles.css -o ./static/styles.css --minify

# Database migrations
migrate: ## Apply database migrations
	goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down: ## Roll back the last migration
	goose -dir migrations postgres "$$DATABASE_URL" down

migrate-status: ## Show migration status
	goose -dir migrations postgres "$$DATABASE_URL" status

# Testing
test: templ tail-prod ## Generate assets and run Go tests
	go test -v ./...

# Docker (production)
# CI sets METADATA_FILE to read the pushed image digest from buildx's metadata.
docker-buildx: ensure-image-tag templ tail-prod ## Build and push multi-arch Docker image using buildx
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(REVISION) \
		--build-arg SOURCE_URL=$(SOURCE_URL) \
		--tag $(REGISTRY)/$(IMAGE_REPO):$(TAG) \
		--tag $(REGISTRY)/$(IMAGE_REPO):latest \
		$(if $(METADATA_FILE),--metadata-file "$(METADATA_FILE)") \
		--push \
		.

help: ## Show this help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-20s %s\n", $$1, $$2} END {printf "\n"}' $(MAKEFILE_LIST)


