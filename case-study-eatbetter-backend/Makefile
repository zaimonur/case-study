.PHONY: up down logs build test vet verify migrate-up migrate-down

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f api db

build:
	go build -o ./bin/api ./cmd/api

test:
	go test -race ./...

vet:
	go vet ./...

verify: test vet build

migrate-up:
	docker compose run --rm migrate up

migrate-down:
	docker compose run --rm migrate down 1
