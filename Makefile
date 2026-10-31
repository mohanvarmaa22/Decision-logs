VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX  ?= $(HOME)/.local

.PHONY: build test vet fmt check install uninstall

build:
	go build -ldflags "$(LDFLAGS)" -o bin/decision-log ./cmd/decision-log

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# check is what to run before committing.
check: vet test
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	claude plugin validate ./plugin

install: build
	install -d $(PREFIX)/bin
	install -m 0755 bin/decision-log $(PREFIX)/bin/decision-log

uninstall:
	rm -f $(PREFIX)/bin/decision-log
