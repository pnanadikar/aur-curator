APP := aur-curator
CMD := ./cmd/aur-curator
BIN_DIR := bin
BIN := $(BIN_DIR)/$(APP)
BIN_DEBUG := $(BIN_DIR)/$(APP)-debug
CONFIG ?=
PREFIX ?= $(HOME)/.local

# Version from the nearest vMAJOR.MINOR.PATCH tag: "v1.0.0" on a tagged
# commit, "v1.0.0-5-g1a2b3c4" after it, "-dirty" with uncommitted changes.
# Non-version tags (e.g. pre-351-install) are ignored.
VERSION := $(shell git describe --tags --match 'v[0-9]*' --dirty --always)
VERSION_LDFLAG := -X 'github.com/pramod/aur-curator/internal/version.Version=$(VERSION)'

.PHONY: help build debug-build install uninstall test-build test-ui test test-race lint tracecheck vet vet-all run fmt clean

help:
	@printf '%s\n' \
		'Available targets:' \
		'  build        Build the optimized production app to $(BIN)' \
		'  debug-build  Build the unoptimized debug app to $(BIN_DEBUG)' \
		'  install      Build, then install $(APP) to $(PREFIX)/bin (override with PREFIX=...)' \
		'  uninstall    Remove $(PREFIX)/bin/$(APP)' \
		'  test-build   Build to $(BIN_DEBUG) and run make test && make lint' \
		'  test-ui      Run go test ./internal/ui/... -v' \
		'  test         Run go test ./...' \
		'  test-race    Run go test -race ./... (needs cgo: gcc from base-devel)' \
		'  lint         Run tracecheck, then golangci-lint run ./...' \
		'  tracecheck   Report trace.Log, activitylog.Log and procgroup.Run calls with unpaired key/value args' \
		'  vet          Run go vet ./...' \
		'  vet-all      Run vet, build and test' \
		'  run          Run the app; pass CONFIG=/path/to/aur-curator.jsonc' \
		'  fmt          Run gofmt across cmd/, internal/ and tools/' \
		'  clean        Remove built artifacts in $(BIN_DIR)/' \
		'' \
		'Version: $(VERSION)'

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w $(VERSION_LDFLAG)" -o $(BIN) $(CMD)

debug-build:
	@mkdir -p $(BIN_DIR)
	go build -gcflags="all=-N -l" -ldflags="$(VERSION_LDFLAG)" -o $(BIN_DEBUG) $(CMD)

install: build
	install -Dm755 $(BIN) $(PREFIX)/bin/$(APP)

uninstall:
	rm -f $(PREFIX)/bin/$(APP)

test-build: debug-build
	@$(MAKE) test
	@$(MAKE) lint

test-ui:
	go test ./internal/ui/... -v

test:
	go test ./...

test-race:
	CGO_ENABLED=1 go test -race ./...

lint: tracecheck
	golangci-lint run ./...

tracecheck:
	go run ./tools/tracecheck .

vet:
	go vet ./...

vet-all: vet build test

run:
	@if [ -z "$(CONFIG)" ]; then \
		printf '%s\n' 'CONFIG is required, for example: make run CONFIG=~/.config/aur-curator/aur-curator.jsonc'; \
		exit 1; \
	fi
	go run -ldflags="$(VERSION_LDFLAG)" $(CMD) $(CONFIG)

fmt:
	gofmt -w cmd internal tools

clean:
	rm -rf $(BIN_DIR)
