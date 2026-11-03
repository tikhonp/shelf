# <branch>-<sha7>, as CI sets it
APP_VERSION := $(shell b=$$(git rev-parse --abbrev-ref HEAD 2>/dev/null) && c=$$(git rev-parse --short=7 HEAD 2>/dev/null) && echo "$$b-$$c" || echo dev)

.PHONY: check test lint web-check dev manage build-image

# CI passes GOTESTFLAGS=-timeout=30m, because its runner is slower than a laptop.
GOTESTFLAGS ?=
# modernc SQLite is C translated to Go: instrumenting it for -race makes every
# test several times slower and checks only the C library, never shelf's code.
RACEFLAGS = -race -gcflags='modernc.org/...=-race=false'

# gofmt, vet and the tests: everything CI's test job runs, on the host.
check:
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi
	go vet ./...
	$(MAKE) test

test:
	go test $(RACEFLAGS) -count=1 $(GOTESTFLAGS) ./...

# golangci-lint and govulncheck in the Dockerfile's lint stage. An unchanged
# tree that passed once is a cache hit.
lint:
	docker build --target lint --progress=plain .

web-check:
	npm --prefix web ci
	npm --prefix web run typecheck
	npm --prefix web test
	npm --prefix web run build

# The development stack with air hot reload on :8080. .env is optional; see
# .env.example.
dev:
	docker compose up --build

# make manage ARGS="migrate status"
manage:
	docker exec -it shelf shelf manage $(ARGS)

build-image:
	docker buildx build --build-arg APP_VERSION=$(APP_VERSION) -t code.tikhonnnnn.com/tikhon/shelf:local --load .
