BINARY := db
CMD     := ./cmd

-include .env
export

STEPS ?= 1

.PHONY: build migrate migrate-down test clean

build:
	go build -o bin/$(BINARY) $(CMD)

migrate: build
	cd bin && ./$(BINARY)

migrate-down: build
	cd bin && ./$(BINARY) down $(STEPS)

test:
	go test ./...

clean:
	rm -rf bin/
