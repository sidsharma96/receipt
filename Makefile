.PHONY: build test race vet integration lint lint-install fmt fmt-check check docs hooks-install
.DEFAULT_GOAL := build

GOLANGCI_LINT_VERSION := v2.14.0
GOLANGCI_LINT_BIN := .tools/bin/golangci-lint

$(GOLANGCI_LINT_BIN): scripts/install-golangci-lint.sh Makefile
	bash scripts/install-golangci-lint.sh $(GOLANGCI_LINT_VERSION) $(GOLANGCI_LINT_BIN)

lint-install:
	bash scripts/install-golangci-lint.sh $(GOLANGCI_LINT_VERSION) $(GOLANGCI_LINT_BIN)

fmt: $(GOLANGCI_LINT_BIN)
	$(GOLANGCI_LINT_BIN) fmt

fmt-check: $(GOLANGCI_LINT_BIN)
	@lint_format_diff="$$($(GOLANGCI_LINT_BIN) fmt --diff)" || exit $$?; if test -n "$$lint_format_diff"; then printf '%s\n' "$$lint_format_diff"; exit 1; fi

lint: fmt-check
	$(GOLANGCI_LINT_BIN) config verify
	$(GOLANGCI_LINT_BIN) run

check: lint test

# Go 1.22's internal Mach-O linker is incompatible with this machine's newer
# macOS loader. The system linker avoids the missing LC_UUID error.
ifeq ($(shell uname -s),Darwin)
LINKFLAGS := -ldflags=-linkmode=external
endif

build:
	go build $(LINKFLAGS) -o receipt ./cmd/receipt
ifeq ($(shell uname -s),Darwin)
	codesign --force --sign - receipt
endif

test:
	go test $(LINKFLAGS) ./...

race:
	go test -race $(LINKFLAGS) ./...

vet:
	go vet ./...

# Use an explicitly selected disposable database; tests create isolated schemas.
integration:
	@test -n "$$RECEIPT_TEST_DATABASE_URL" || (echo "Set RECEIPT_TEST_DATABASE_URL to a disposable PostgreSQL database"; exit 1)
	go test -count=1 $(LINKFLAGS) ./...

# Build the documentation book (needs mdBook 0.5.4) and run the doc checks.
# mdBook copies every non-Markdown file under docs/ into the site, including
# book.toml itself; remove that copy so the site holds only documentation.
docs:
	mdbook build docs
	rm -f docs/book/book.toml
	go test $(LINKFLAGS) ./tools/docs ./tools/templates
	go run $(LINKFLAGS) ./tools/docs check

# Per-clone setup; Git does not enable versioned hooks automatically.
hooks-install:
	git config --local core.hooksPath .githooks
