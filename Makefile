GO ?= go
APP_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
VERSION_LDFLAG = -X github.com/filipegalo/shelly-energy-dashboard/internal/appinfo.Version=$(APP_VERSION)

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
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(VERSION_LDFLAG)" -o bin/shelly-inventory ./cmd/server
run:
	$(GO) run ./cmd/server
docker-build:
	docker build --build-arg APP_VERSION="$(APP_VERSION)" -t shelly-inventory:local .
compose-up:
	APP_VERSION="$(APP_VERSION)" docker compose up -d --build
mocks:
	docker compose -f docker-compose.yml -f docker-compose.mock.yml up -d --build
