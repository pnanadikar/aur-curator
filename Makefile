APP := aur-curator
CMD := ./cmd/aur-curator
BIN_DIR := bin
BIN := $(BIN_DIR)/$(APP)
BIN_DEBUG := $(BIN_DIR)/$(APP)-debug
CONFIG ?=
PREFIX ?= $(HOME)/.local
# fish completion (#511): installed by make install when fish is present.
FISH_COMPLETION := contrib/completions/$(APP).fish
FISH_COMPLETION_DIR := $(or $(XDG_CONFIG_HOME),$(HOME)/.config)/fish/completions

# Version from the nearest vMAJOR.MINOR.PATCH tag: "v1.0.0" on a tagged
# commit, "v1.0.0-5-g1a2b3c4" after it, "-dirty" with uncommitted changes.
# Non-version tags (e.g. pre-351-install) are ignored.
VERSION := $(shell git describe --tags --match 'v[0-9]*' --dirty --always)
VERSION_LDFLAG := -X 'github.com/pramod/aur-curator/internal/version.Version=$(VERSION)'

.PHONY: help build debug-build install uninstall test-build test-ui test test-race test-sandbox lint tracecheck vuln vet vet-all run fmt clean release

help:
	@printf '%s\n' \
		'Available targets:' \
		'  build        Build the optimized production app to $(BIN)' \
		'  debug-build  Build the unoptimized debug app to $(BIN_DEBUG)' \
		'  install      Build, then install $(APP) to $(PREFIX)/bin (override with PREFIX=...) and the fish completion' \
		'  uninstall    Remove $(PREFIX)/bin/$(APP) and the fish completion' \
		'  test-build   Build to $(BIN_DEBUG) and run make test && make lint' \
		'  test-ui      Run go test ./internal/ui/... -v' \
		'  test         Run go test ./...' \
		'  test-race    Run go test -race ./... (needs cgo: gcc from base-devel)' \
		'  test-sandbox Build hostile fixture PKGBUILDs in the real sandbox (bwrap, makepkg; not as root)' \
		'  lint         Run tracecheck, then golangci-lint run ./... (gosec included)' \
		'  tracecheck   Report trace.Log, activitylog.Log and procgroup.Run calls with unpaired key/value args' \
		'  vuln         Run govulncheck (pinned) over ./...; needs the network' \
		'  vet          Run go vet ./...' \
		'  vet-all      Run vet, build and test' \
		'  run          Run the app; pass CONFIG=/path/to/aur-curator.jsonc' \
		'  fmt          Run gofmt across cmd/, internal/ and tools/' \
		'  clean        Remove built artifacts in $(BIN_DIR)/' \
		'  release      Test, vuln, tag main as RELEASE=vX.Y.Z, push the tag, build, check and install' \
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
	@if ! command -v fish >/dev/null 2>&1; then \
		echo 'fish not found: fish completion not installed'; \
	elif [ "$$(id -u)" = 0 ]; then \
		echo 'running as root: fish completion not installed; as your user, copy $(FISH_COMPLETION) to ~/.config/fish/completions/'; \
	else \
		install -Dm644 $(FISH_COMPLETION) $(FISH_COMPLETION_DIR)/$(APP).fish && \
		echo 'fish completion installed to $(FISH_COMPLETION_DIR)/$(APP).fish'; \
	fi

uninstall:
	rm -f $(PREFIX)/bin/$(APP) $(FISH_COMPLETION_DIR)/$(APP).fish

test-build: debug-build
	@$(MAKE) test
	@$(MAKE) lint

test-ui:
	go test ./internal/ui/... -v

test:
	go test ./...

test-race:
	CGO_ENABLED=1 go test -race ./...

# The adversarial fixtures of P5 (#496): real makepkg in the real bwrap.
# They fail, never skip, when either is missing.
test-sandbox:
	go test -count=1 -tags sandboxfixtures -run 'TestSandboxFixture' -v ./internal/build/

lint: tracecheck
	golangci-lint run ./...

tracecheck:
	go run ./tools/tracecheck .

# vuln reports known vulnerabilities in code aur-curator calls
# (DEVELOPMENT.md § Releases, #493). Pinned so the result does not change
# with the tool; make release runs it before tagging.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

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

# release tags main as $(RELEASE), pushes the tag to Gitea (origin), then
# builds, checks the version and installs (DEVELOPMENT.md § Releases,
# #464). Every check runs before anything is tagged, make test and make
# vuln (#493) included. Builds and installs run in sub-makes, so VERSION
# is read again after the tag exists.
release:
	@set -e; \
	r='$(RELEASE)'; \
	fail() { printf 'make release: %s\n' "$$1" >&2; exit 1; }; \
	after() { fail "$$1. Tag $$r is already on origin: finish by hand, do not run make release with $$r again"; }; \
	printf '%s\n' "$$r" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' \
		|| fail "RELEASE must be vMAJOR.MINOR.PATCH, e.g. make release RELEASE=v1.2.3"; \
	[ "$$(git branch --show-current)" = main ] || fail "not on main"; \
	[ -z "$$(git status --porcelain)" ] || fail "the working tree has uncommitted changes"; \
	git fetch --quiet origin main; \
	[ "$$(git rev-parse main)" = "$$(git rev-parse origin/main)" ] \
		|| fail "main differs from origin/main: pull (or push) first"; \
	! git rev-parse --quiet --verify "refs/tags/$$r" >/dev/null || fail "tag $$r already exists locally"; \
	[ -z "$$(git ls-remote --tags origin "refs/tags/$$r")" ] || fail "tag $$r already exists on origin"; \
	$(MAKE) test || fail "tests failed; nothing was tagged"; \
	$(MAKE) vuln || fail "govulncheck reports a vulnerability, or could not run; nothing was tagged (DEVELOPMENT.md § Releases)"; \
	git tag -a "$$r" -m "$$r"; \
	git push origin "$$r" \
		|| fail "pushing the tag failed; it exists only locally: retry with git push origin $$r, or remove it with git tag -d $$r"; \
	$(MAKE) build || after "build failed"; \
	v="$$(./$(BIN) --version)"; \
	[ "$$v" = "$$r" ] || after "$(BIN) --version prints $$v, not $$r"; \
	$(MAKE) install || after "install failed"; \
	printf 'Released %s and installed it to %s/bin.\n' "$$r" "$(PREFIX)"
