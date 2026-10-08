.PHONY: build test race vet integration

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
