# BNPL platform developer tasks. All commands run from the repository root.
SHELL := /usr/bin/env bash

BACKEND := backend
FRONTEND := frontend

# Default local DSN; override on the command line as `make migrate-up DB_DSN=...`.
MYSQL_HOST     ?= 127.0.0.1
MYSQL_PORT     ?= 3306
MYSQL_DATABASE ?= bnpl
MYSQL_USER     ?= bnpl
MYSQL_PASSWORD ?= bnplpw
# Application DSN: no multiStatements.
DB_DSN ?= $(MYSQL_USER):$(MYSQL_PASSWORD)@tcp($(MYSQL_HOST):$(MYSQL_PORT))/$(MYSQL_DATABASE)?parseTime=true&loc=UTC&charset=utf8mb4&collation=utf8mb4_0900_ai_ci
# Migration DSN: golang-migrate requires multiStatements.
MIGRATE_DSN ?= $(MYSQL_USER):$(MYSQL_PASSWORD)@tcp($(MYSQL_HOST):$(MYSQL_PORT))/$(MYSQL_DATABASE)?parseTime=true&loc=UTC&multiStatements=true&charset=utf8mb4&collation=utf8mb4_0900_ai_ci

.PHONY: help
help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: compose-up
compose-up: ## Start MySQL 8 in the background.
	docker compose up -d

.PHONY: compose-down
compose-down: ## Stop and remove containers (keeps the volume).
	docker compose down

.PHONY: compose-wait
compose-wait: ## Block until MySQL reports healthy.
	@until [ "$$(docker inspect -f '{{.State.Health.Status}}' bnpl-mysql 2>/dev/null)" = "healthy" ]; do \
		echo "waiting for mysql..."; sleep 2; \
	done; echo "mysql healthy"

.PHONY: migrate-up
migrate-up: ## Apply all up migrations.
	cd $(BACKEND) && migrate -path migrations -database "mysql://$(MIGRATE_DSN)" up

.PHONY: migrate-down
migrate-down: ## Roll back all migrations.
	cd $(BACKEND) && migrate -path migrations -database "mysql://$(MIGRATE_DSN)" down -all

.PHONY: sqlc
sqlc: ## Regenerate type-safe SQL.
	cd $(BACKEND) && sqlc generate

.PHONY: build
build: ## Build all Go packages.
	cd $(BACKEND) && go build ./...

.PHONY: lint
lint: ## Lint the backend.
	cd $(BACKEND) && golangci-lint run ./...

.PHONY: test
test: ## Run backend unit tests with the race detector.
	cd $(BACKEND) && go test -race ./internal/...

.PHONY: test-integration
test-integration: ## Run backend integration tests (testcontainers).
	cd $(BACKEND) && go test -race -tags=integration ./tests/...

.PHONY: verify
verify: build lint test ## Build, lint and unit-test the backend.

.PHONY: run-checkout
run-checkout: ## Run the Checkout service against the local database.
	cd $(BACKEND) && DB_DSN="$(DB_DSN)" go run ./cmd/checkout

.PHONY: run-underwriting
run-underwriting: ## Run the Underwriting service against the local database.
	cd $(BACKEND) && DB_DSN="$(DB_DSN)" go run ./cmd/underwriting

.PHONY: frontend-install
frontend-install: ## Install frontend dependencies.
	npm --prefix $(FRONTEND) install

.PHONY: frontend-lint
frontend-lint: ## Lint the frontend.
	npm --prefix $(FRONTEND) run lint

.PHONY: frontend-build
frontend-build: ## Build the frontend.
	npm --prefix $(FRONTEND) run build
