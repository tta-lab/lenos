VERSION ?= $(shell git describe --long 2>/dev/null || echo "")

CGO_ENABLED ?= 0
export CGO_ENABLED

GOEXPERIMENT ?= greenteagc
export GOEXPERIMENT

LDFLAGS = $(if $(VERSION),-ldflags="-X github.com/tta-lab/lenos/internal/version.Version=$(VERSION)",)

.PHONY: build
build:
	go build -v $(LDFLAGS) .

.PHONY: install
install:
	go install $(LDFLAGS) -v .

.PHONY: test
test:
	go test -race -failfast ./...

.PHONY: fmt
fmt:
	gofumpt -w .

.PHONY: modernize
modernize:
	go run golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@latest -fix -test ./...

.PHONY: dev
dev:
	LENOS_PROFILE=true go run .

.PHONY: lint
lint:
	./scripts/check_log_capitalization.sh
	golangci-lint run --path-mode=abs --config=".golangci.yml" --timeout=5m

.PHONY: lint-fix
lint-fix:
	golangci-lint run --path-mode=abs --config=".golangci.yml" --timeout=5m --fix

.PHONY: schema
schema:
	go run . schema > schema.json
	@echo "Generated schema.json"

.PHONY: hyper
hyper:
	go generate ./internal/agent/hyper/...

.PHONY: sqlc
sqlc:
	sqlc generate
