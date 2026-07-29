.PHONY: help run build test test-integration lint fmt vuln tidy migrate-up migrate-down migrate-status migrate-create docker-build up up-fg logs down clean

# Host ports the stack publishes. Override when they are already taken:
#   make up API_PORT=8081 DB_PORT=5433
API_PORT ?= 8080
DB_PORT  ?= 5432
export API_PORT
export DB_PORT

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

run: ## Run the API server
	go run . serve

build: ## Build the binary into ./bin
	go build -o bin/passwordless-auth-service .

test: ## Run tests with race detector and coverage
	go test ./... -race -cover

test-integration: ## Run the tagged suite against real Postgres (needs Docker)
	go test -tags integration ./... -count=1

openapi: ## Regenerate the committed OpenAPI spec from the handlers
	go run . openapi -o docs/openapi.yaml

openapi-check: ## Fail if the committed spec is stale (what CI runs)
	@go run . openapi -o docs/openapi.yaml
	@git diff --exit-code -- docs/openapi.yaml \
		|| { echo "docs/openapi.yaml is stale — run 'make openapi' and commit"; exit 1; }

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format code with gofumpt
	gofumpt -w .

vuln: ## Scan for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

tidy: ## Tidy go.mod / go.sum
	go mod tidy

migrate-up: ## Apply all up migrations (needs DATABASE_URL)
	go run . migrate up

migrate-down: ## Roll back the last migration (needs DATABASE_URL)
	go run . migrate down

migrate-status: ## Show migration status (needs DATABASE_URL)
	go run . migrate status

migrate-create: ## Create a migration: make migrate-create name=add_users (needs goose CLI)
	goose -dir ./migrations create $(name) sql

docker-build: ## Build the Docker image
	docker build -t passwordless-auth-service .

up: ## Start the stack (Postgres, migrations, API) and wait until it answers
	@docker compose up --build -d
	@printf 'waiting for the API on :$(API_PORT)'
	@for i in $$(seq 1 60); do \
		if curl -fsS http://localhost:$(API_PORT)/healthz >/dev/null 2>&1; then \
			printf '\n\n  API       http://localhost:$(API_PORT)\n'; \
			printf '  Docs      http://localhost:$(API_PORT)/docs\n'; \
			printf '  OTP code  make logs\n'; \
			printf '  Stop      make down\n\n'; \
			exit 0; \
		fi; \
		printf '.'; sleep 1; \
	done; \
	printf '\n\nthe API never became healthy. Recent logs:\n\n'; \
	docker compose logs --tail=40; \
	exit 1

up-fg: ## Same as up, but in the foreground (Ctrl-C to stop)
	docker compose up --build

logs: ## Follow API logs — the stub sender writes the OTP here
	docker compose logs -f api

down: ## Stop the stack, keeping the database volume
	docker compose down

clean: ## Stop the stack and delete its database volume
	docker compose down -v
