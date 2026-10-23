VERSION=v0.96.0-teamgram-server
BUILD=`date +%F`
SHELL := /bin/bash
BASEDIR = $(shell pwd)

# build with verison infos
versionDir="github.com/teamgram/marmota/pkg/version"
gitTag=$(shell if [ "`git describe --tags --abbrev=0 2>/dev/null`" != "" ];then git describe --tags --abbrev=0; else git log --pretty=format:'%h' -n 1; fi)
gitBranch=$(shell git rev-parse --abbrev-ref HEAD)
buildDate=$(shell TZ=Asia/Shanghai date +%FT%T%z)
gitCommit=$(shell git rev-parse --short HEAD)
gitTreeState=$(shell if git status|grep -q 'clean';then echo clean; else echo dirty; fi)

ldflags="-s -w -X ${versionDir}.gitTag=${gitTag} -X ${versionDir}.buildDate=${buildDate} -X ${versionDir}.gitCommit=${gitCommit} -X ${versionDir}.gitTreeState=${gitTreeState} -X ${versionDir}.version=${VERSION} -X ${versionDir}.gitBranch=${gitBranch}"

# `make` alone builds the service binaries; the checks are below `clean`.
.DEFAULT_GOAL := all

# Versions of the check tools: pinned here and nowhere else. CI reads them
# with `make print-tool-versions`. The Go tools run through
# `go run module@version` (built with the go.mod toolchain), so nothing has to
# be installed; shellcheck is a release binary that make downloads into .tools/
# and checks against the SHA-256 below.
GOLANGCI_LINT_VERSION := v2.15.0
GOVULNCHECK_VERSION   := v1.9.0
ACTIONLINT_VERSION    := v1.7.12
SHELLCHECK_VERSION    := v0.11.0
# SHA-256 of shellcheck-$(SHELLCHECK_VERSION).<os>.<arch>.tar.gz of the
# GitHub release (the digests the release page lists).
SHELLCHECK_SHA256_darwin.aarch64 := 339b930feb1ea764467013cc1f72d09cd6b869ebf1013296ba9055ab2ffbd26f
SHELLCHECK_SHA256_darwin.x86_64  := c2c15e08df0e8fbc374c335b230a7ee958c313fa5714817a59aa59f1aa594f51
SHELLCHECK_SHA256_linux.aarch64  := 68a8133197a50beb8803f8d42f9908d1af1c5540d4bb05fdfca8c1fa47decefc
SHELLCHECK_SHA256_linux.x86_64   := b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6

all: idgen status dfs media authsession biz msg sync bff session gnetway

idgen:
	@echo "build idgen..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/idgen -tags=jsoniter app/service/idgen/cmd/idgen/*.go

status:
	@echo "build status..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/status -tags=jsoniter app/service/status/cmd/status/*.go

dfs:
	@echo "build dfs..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/dfs -tags=jsoniter app/service/dfs/cmd/dfs/*.go

media:
	@echo "build media..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/media -tags=jsoniter app/service/media/cmd/media/*.go

authsession:
	@echo "build authsession..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/authsession -tags=jsoniter app/service/authsession/cmd/authsession/*.go

biz:
	@echo "build biz..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/biz -tags=jsoniter app/service/biz/biz/cmd/biz/*.go

#msgtransfer:
#	@echo "build msgtransfer..."
#	@go build -ldflags ${ldflags} -o ${INSTALL}/bin/msgtransfer -tags=jsoniter app/messenger/msg/msgtransfer/cmd/msgtransfer/*.go

msg:
	@echo "build msg..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/msg -tags=jsoniter app/messenger/msg/cmd/msg/*.go

sync:
	@echo "build sync..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/sync -tags=jsoniter app/messenger/sync/cmd/sync/*.go

bff:
	@echo "build bff..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/bff -tags=jsoniter app/bff/bff/cmd/bff/*.go

session:
	@echo "build session..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/session -tags=jsoniter app/interface/session/cmd/session/*.go

gnetway:
	@echo "build gnetway..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/gnetway -tags=jsoniter app/interface/gnetway/cmd/gnetway/*.go

httpserver:
	@echo "build httpserver..."
	@go build -ldflags ${ldflags} -o teamgramd/bin/httpserver -tags=jsoniter app/interface/httpserver/cmd/httpserver/*.go

clean:
	@rm -rf teamgramd/bin/idgen
	@rm -rf teamgramd/bin/status
	@rm -rf teamgramd/bin/dfs
	@rm -rf teamgramd/bin/media
	@rm -rf teamgramd/bin/authsession
	@rm -rf teamgramd/bin/biz
	@rm -rf teamgramd/bin/msgtransfer
	@rm -rf teamgramd/bin/msg
	@rm -rf teamgramd/bin/sync
	@rm -rf teamgramd/bin/bff
	@rm -rf teamgramd/bin/session
	@rm -rf teamgramd/bin/gnetway
	@rm -rf teamgramd/bin/httpserver

# ----------------------------------------------------------------------------
# Checks: the same sequence runs in CI (.github/workflows/ci.yml, `make ci`).
# See docs/ops/testing.md.

GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK   := go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
ACTIONLINT    := go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

SHELLCHECK_PLATFORM := $(shell uname -s | tr A-Z a-z).$(shell uname -m | sed 's/^arm64$$/aarch64/')
SHELLCHECK          := $(CURDIR)/.tools/shellcheck-$(SHELLCHECK_VERSION)/shellcheck
# Our shell scripts (the upstream ones in teamgramd/bin are not checked).
SHELL_SCRIPTS := test/e2e/run.sh teamgramd/deploy/kafka/topics.sh

# The vendored proto module is a Go module of its own: `./...` at the root
# does not reach it, so every check runs there as well.
# third_party/teamgram-marmota (ADR 0009, 0011) is not checked on its own: its
# upstream code was never vetted or linted, govulncheck at the root covers
# the dependency versions our binaries link, and our patches there are
# exercised by the root module's tests (pkg/userbox).
PROTO_DIR := third_party/teamgram-proto
# Lint reports only code changed since the upstream base: 708fb953 for the root
# module (.golangci.yml), the unmodified vendored copy for the proto module —
# the commit that added its go.mod, looked up so that it survives history
# rewrites (needs full history: CI checks out with fetch-depth: 0).
PROTO_LINT_BASE := $(shell git log --format=%H --diff-filter=A -- $(PROTO_DIR)/go.mod | tail -n 1)

# `unreachable` is off: the mtprotoc codecs (codec_schema.tl.pb.go) have dead
# code after returns, and generated code is not edited by hand. All other vet
# analyzers run, on the integration and e2e tests too (build tags).
VET_FLAGS := -unreachable=false -tags=integration,e2e

# Race tests run on our own code (the rest of the tree has few tests).
RACE_PKGS := ./pkg/... ./tools/... ./app/messenger/... \
	./app/interface/gnetway/... ./app/interface/session/... \
	./app/service/authsession/... ./app/bff/authorization/... ./app/bff/account/... \
	./app/service/dfs/internal/ffmpegutil/...

COVERAGE_OUT := coverage.out
API_COVERAGE_DOC := docs/project/api-coverage.md

# Packages with integration tests (build tag integration): every directory
# holding a test file with that tag, so new ones are picked up.
INTEGRATION_PKGS = $(shell grep -rl --include='*_test.go' -e '^//go:build integration' app pkg tools | xargs -n1 dirname | sort -u | sed 's|^|./|')
# Packages tested at once: each starts its own MySQL (pkg/userbox two), and
# more at once run a Docker VM of 6 GB out of memory.
INTEGRATION_PARALLEL ?= 4

.PHONY: build vet test test-race test-integration e2e lint vuln coverage apicoverage apicoverage-summary apicoverage-check actionlint shellcheck ci print-tool-versions

build:
	go build ./...
	cd $(PROTO_DIR) && go build ./...

vet:
	go vet $(VET_FLAGS) ./...
	cd $(PROTO_DIR) && go vet $(VET_FLAGS) ./...

test:
	go test -count=1 ./...
	cd $(PROTO_DIR) && go test -count=1 ./...

test-race:
	go test -race -count=1 $(RACE_PKGS)
	cd $(PROTO_DIR) && go test -race -count=1 ./...

# Integration tests on real MySQL (production schema), Redis and Kafka in
# containers (testcontainers-go, pkg/testinfra). Needs Docker. Without
# DOCKER_HOST the endpoint of the current docker context is used; for a
# runtime in a VM under the home directory (colima, OrbStack, Docker Desktop)
# the reaper container gets the VM's socket path.
test-integration:
	@set -e; \
	if [ -z "$$DOCKER_HOST" ]; then \
		DOCKER_HOST=$$(docker context inspect --format '{{.Endpoints.docker.Host}}'); export DOCKER_HOST; \
		case "$$DOCKER_HOST" in \
		unix://$$HOME/*) export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=$${TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE:-/var/run/docker.sock} ;; \
		esac; \
	fi; \
	echo "go test -tags integration -p $(INTEGRATION_PARALLEL) $(INTEGRATION_PKGS)"; \
	go test -tags integration -race -count=1 -p $(INTEGRATION_PARALLEL) -timeout 20m $(INTEGRATION_PKGS)

# End-to-end test of the whole stack through docker compose: throwaway
# secrets, build, start, test, service logs on failure (in .e2e/logs), stop.
# See test/e2e/run.sh for the steps one by one.
e2e:
	test/e2e/run.sh all

lint:
	$(GOLANGCI_LINT) run ./...
	cd $(PROTO_DIR) && $(GOLANGCI_LINT) run --config $(CURDIR)/.golangci.yml --new-from-rev $(PROTO_LINT_BASE) ./...

vuln:
	$(GOVULNCHECK) ./...
	cd $(PROTO_DIR) && $(GOVULNCHECK) ./...

coverage:
	go test -count=1 -covermode=atomic -coverprofile=$(COVERAGE_OUT) ./...
	cd $(PROTO_DIR) && go test -count=1 -covermode=atomic -coverprofile=$(COVERAGE_OUT) ./...
	@set -o pipefail; echo "root module:  $$(go tool cover -func=$(COVERAGE_OUT) | tail -n 1)"
	@set -o pipefail; cd $(PROTO_DIR) && echo "proto module: $$(go tool cover -func=$(COVERAGE_OUT) | tail -n 1)"
	@echo "HTML report: go tool cover -html=$(COVERAGE_OUT)"

# Regenerates docs/project/api-coverage.md.
apicoverage:
	go run ./tools/apicoverage

apicoverage-summary:
	go run ./tools/apicoverage -summary

# Fails when docs/project/api-coverage.md is not what the tool generates from
# the current code (run `make apicoverage` and commit the result). The fresh
# output goes to a temporary file, so the check works on a dirty working tree
# and changes nothing.
apicoverage-check:
	@tmp=$$(mktemp) && trap 'rm -f "$$tmp"' EXIT && \
	go run ./tools/apicoverage -out "$$tmp" >/dev/null && \
	if ! diff -u $(API_COVERAGE_DOC) "$$tmp"; then \
		echo "$(API_COVERAGE_DOC) is out of date: run 'make apicoverage' and commit it" >&2; exit 1; \
	fi

# actionlint runs shellcheck on the run: blocks only if it finds one; it gets
# the pinned one, as in CI.
actionlint: $(SHELLCHECK)
	$(ACTIONLINT) -shellcheck=$(SHELLCHECK)

shellcheck: $(SHELLCHECK)
	$(SHELLCHECK) $(SHELL_SCRIPTS)

# Downloads the pinned shellcheck release for this platform and checks its
# SHA-256 before unpacking it.
$(SHELLCHECK):
	@set -e; \
	sum='$(SHELLCHECK_SHA256_$(SHELLCHECK_PLATFORM))'; \
	if [ -z "$$sum" ]; then \
		echo "no pinned shellcheck $(SHELLCHECK_VERSION) build for $(SHELLCHECK_PLATFORM): add its SHA-256 to the Makefile" >&2; exit 1; \
	fi; \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	echo "download shellcheck $(SHELLCHECK_VERSION) ($(SHELLCHECK_PLATFORM))"; \
	curl -fsSL --retry 3 -o "$$tmp/shellcheck.tar.gz" \
		https://github.com/koalaman/shellcheck/releases/download/$(SHELLCHECK_VERSION)/shellcheck-$(SHELLCHECK_VERSION).$(SHELLCHECK_PLATFORM).tar.gz; \
	got=$$( (sha256sum "$$tmp/shellcheck.tar.gz" 2>/dev/null || shasum -a 256 "$$tmp/shellcheck.tar.gz") | cut -d' ' -f1); \
	if [ "$$got" != "$$sum" ]; then \
		echo "shellcheck archive SHA-256 is $$got, want $$sum" >&2; exit 1; \
	fi; \
	tar -xzf "$$tmp/shellcheck.tar.gz" -C "$$tmp"; \
	mkdir -p $(@D); \
	mv "$$tmp/shellcheck-$(SHELLCHECK_VERSION)/shellcheck" $@

# Everything CI runs, in the same order.
ci:
	$(MAKE) build
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) test-race
	$(MAKE) lint
	$(MAKE) vuln
	$(MAKE) apicoverage-summary
	$(MAKE) apicoverage-check
	$(MAKE) actionlint
	$(MAKE) shellcheck

# key=value lines for $GITHUB_OUTPUT (CI reads the pinned versions from here).
print-tool-versions:
	@echo "golangci_lint=$(GOLANGCI_LINT_VERSION)"
	@echo "proto_lint_base=$(PROTO_LINT_BASE)"
