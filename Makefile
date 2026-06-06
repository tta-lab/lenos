CGO_ENABLED ?= 0
export CGO_ENABLED

GOEXPERIMENT ?= greenteagc
export GOEXPERIMENT

.PHONY: build
build:
	go build -v .

.PHONY: install
install:
	go install .

.PHONY: test
test:
	gotestsum --format testname -- -race -failfast ./...

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
