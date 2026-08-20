.PHONY: up down logs build build-importer build-localizer build-search-evaluator test vet verify migrate-up migrate-down

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

build-search-evaluator:
	go build -o ./bin/food-search-eval ./cmd/food-search-eval

test:
	go test -race ./...

vet:
	go vet ./...

verify: test vet build build-importer build-localizer build-search-evaluator

migrate-up:
	docker compose run --rm migrate up

migrate-down:
	docker compose run --rm migrate down 1
