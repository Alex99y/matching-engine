BINARY := core
CMD     := ./cmd

-include .env
export

.PHONY: build test clean

build:
	go build -o bin/$(BINARY) $(CMD)

run:
	go run $(CMD)

test:
	go test ./...

clean:
	rm -rf bin/
