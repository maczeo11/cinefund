.PHONY: help up down migrate seed test test-int lint proto topics run-api run-dispatcher run-transcoder

COMPOSE := docker compose -f deploy/docker-compose.yml

help:           ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

up:             ## start all infrastructure
	$(COMPOSE) up -d

down:           ## stop infrastructure, keep volumes
	$(COMPOSE) down

nuke:           ## stop infrastructure and delete volumes
	$(COMPOSE) down -v

logs:           ## tail infrastructure logs
	$(COMPOSE) logs -f

migrate:        ## apply schema migrations
	go run ./cmd/migrate up

migrate-down:   ## roll back the most recent migration
	go run ./cmd/migrate down

seed:           ## idempotent dev data
	go run ./cmd/seed

run-api:        ## run the HTTP API
	go run ./cmd/api

run-dispatcher: ## run the outbox -> Kafka dispatcher
	go run ./cmd/dispatcher

run-transcoder: ## run the Kafka -> FFmpeg worker pool
	go run ./cmd/transcoder

test:           ## unit tests, no docker (no race detector - see test-race)
	go test ./... -short

# The race detector needs cgo with a 64-bit C toolchain. The MinGW on this
# machine is 32-bit ("sorry, unimplemented: 64-bit mode not compiled in"), so
# the concurrency tests run in a Linux container instead. That is not a
# workaround to feel bad about: P2 and P5 are only meaningful under -race, and
# this keeps them one command away without installing a second compiler.
test-race:      ## unit tests WITH the race detector, via docker
	docker run --rm -v "$(CURDIR):/src" -w /src golang:1.26 go test ./... -short -race -count=1

test-int:       ## integration tests, needs docker
	go test ./... -tags=integration

lint:           ## golangci-lint
	golangci-lint run

sample:         ## generate the test fixture video
	@mkdir -p testdata
	ffmpeg -f lavfi -i testsrc=duration=10:size=1920x1080:rate=24 \
	       -f lavfi -i sine=frequency=440:duration=10 \
	       -c:v libx264 -c:a aac -shortest testdata/sample_1080p.mp4
