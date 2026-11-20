SHELL := /bin/sh

# go-arch-lint runs through go run at a pinned version so it never enters go.mod.
GO_ARCH_LINT := github.com/fe3dback/go-arch-lint@v1.19.2

.PHONY: check build fmt e2e

# check is the acceptance command for every change. Each step stops the target
# on failure. gofmt skips .claude/, where agent worktrees live.
check:
	@echo "==> gofmt"
	@out=$$(find . -path ./.claude -prune -o -name '*.go' -print | xargs gofmt -l) || exit 1; \
	if [ -n "$$out" ]; then \
		echo "gofmt: these files need formatting (run make fmt):"; \
		echo "$$out"; \
		exit 1; \
	fi
	@echo "==> vet"
	go vet ./...
	@echo "==> build"
	go build ./...
	@echo "==> test"
	go test -race ./...
	@echo "==> arch-lint"
	go run $(GO_ARCH_LINT) check --output-color=false
	@echo "==> import-check"
	sh scripts/check-imports.sh

# e2e compares the timelines of two public fixture repositories with
# git pkgs history. It needs the network and git-pkgs on PATH, so check leaves
# it out.
e2e:
	go test -tags e2e -race -count=1 -run 'TestParity|TestAdvisoryParity' -v -timeout 20m ./cmd/upw/

build:
	go build -o bin/upw ./cmd/upw

fmt:
	gofmt -w .
