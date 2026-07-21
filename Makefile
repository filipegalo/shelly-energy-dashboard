GO ?= go

.PHONY: assets fmt test vet build run docker-build compose-up mocks
assets:
	npm ci
	npm run assets
fmt:
	$(GO) fmt ./...
test:
	$(GO) test -race -cover ./...
vet:
	$(GO) vet ./...
build: assets
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/shelly-inventory ./cmd/server
run:
	$(GO) run ./cmd/server
docker-build:
	docker build -t shelly-inventory:local .
compose-up:
	docker compose up -d --build
mocks:
	docker compose -f docker-compose.yml -f docker-compose.mock.yml up -d --build
