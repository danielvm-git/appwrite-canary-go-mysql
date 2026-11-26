VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
LDFLAGS = -ldflags "-X main.buildVersion=$(VERSION)"
GO      ?= go

.PHONY: build build-function test vet fmt run migrate smoke clean

build:
	$(GO) build $(LDFLAGS) -o bin/api ./cmd/api

# The function entrypoint exactly as the runtime builds it.
build-function:
	$(GO) build $(LDFLAGS) -o bin/main main.go

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

# Local run: needs a reachable MySQL and the endpoint CA.
#   MYSQL_PASSWORD=... MYSQL_CA_PATH=/etc/mysql-heatwave/ca.pem ALLOW_DEV_USER=true MIGRATE_TOKEN=dev make run
run:
	ALLOW_DEV_USER=true $(GO) run ./cmd/api

# Schema changes go through the running API (embedded SQL). There is no local
# mysql client in the happy path, and deploys only happen in CI.
migrate:
	MIGRATE_TOKEN=$${MIGRATE_TOKEN:-dev} BASE=$${BASE:-http://127.0.0.1:3000} scripts/migrate.sh

smoke:
	scripts/smoke.sh

clean:
	rm -rf bin
