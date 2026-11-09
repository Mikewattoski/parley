.RECIPEPREFIX = >
.PHONY: run test test-fast build lint

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
