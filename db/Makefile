BINARY := db
CMD     := ./cmd

-include .env
export

.PHONY: build migrate test clean

build:
	go build -o bin/$(BINARY) $(CMD)

migrate: build
	cd bin && ./$(BINARY)

test:
	go test ./...

clean:
	rm -rf bin/
