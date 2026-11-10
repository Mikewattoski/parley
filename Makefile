.RECIPEPREFIX = >
-include .env
export

.PHONY: up down reset logs migrate psql run test test-fast build lint

up:
> docker compose up -d
> docker compose logs migrate

down:
> docker compose down

reset:
> docker compose down -v

logs:
> docker compose logs -f

migrate:
> docker compose run --rm migrate

psql:
> docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

run:
> go run ./cmd/server

test:
> go test -race ./...

test-fast:
> go test ./...

build:
> go build -o bin/parley ./cmd/server

lint:
> golangci-lint run ./...

.PHONY: web-install web-dev web-test web-build

web-install:
> cd web && npm install

web-dev:
> cd web && npm run dev

web-test:
> cd web && npm test

web-build:
> cd web && npm run build
