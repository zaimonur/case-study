.PHONY: up down logs build build-importer build-localizer test vet verify migrate-up migrate-down

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f api db

build:
	go build -o ./bin/api ./cmd/api

build-importer:
	go build -o ./bin/usda-import ./cmd/usda-import

build-localizer:
	go build -o ./bin/usda-localize ./cmd/usda-localize

test:
	go test -race ./...

vet:
	go vet ./...

verify: test vet build build-importer build-localizer

migrate-up:
	docker compose run --rm migrate up

migrate-down:
	docker compose run --rm migrate down 1
