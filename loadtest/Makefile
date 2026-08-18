BINARIES := latency-ack latency-match latency-cancel

-include .env
export

.PHONY: build test clean run-ack run-match run-cancel run-all

build:
	@for b in $(BINARIES); do \
		go build -o bin/$$b ./cmd/$$b; \
	done

test:
	go test ./...

clean:
	rm -rf bin/

run-ack: build
	LOADTEST_LEVEL=$(LEVEL) ./bin/latency-ack

run-match: build
	LOADTEST_LEVEL=$(LEVEL) ./bin/latency-match

run-cancel: build
	LOADTEST_LEVEL=$(LEVEL) ./bin/latency-cancel

run-all: build
	LOADTEST_LEVEL=$(LEVEL) ./bin/latency-ack
	LOADTEST_LEVEL=$(LEVEL) ./bin/latency-match
	LOADTEST_LEVEL=$(LEVEL) ./bin/latency-cancel
