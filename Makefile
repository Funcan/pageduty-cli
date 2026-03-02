BINARY := pagerduty
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X pagerduty/cmd.Version=$(VERSION)"

.PHONY: default build format lint test clean

default: format lint test build

build:
	go build $(LDFLAGS) -o $(BINARY) .

format:
	gofmt -w .

lint:
	go vet ./...

test:
	go test --coverage ./...

clean:
	rm -f $(BINARY)
