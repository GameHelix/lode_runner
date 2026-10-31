GOCACHE ?= $(CURDIR)/.cache/go-build
export GOCACHE

.PHONY: test check radar

test:
	go test -race ./...

check:
	go vet ./...
	go test -race ./...

radar:
	go run ./trader/cmd/trader radar
