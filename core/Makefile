BINARY := core
CMD     := ./cmd

.PHONY: build test clean

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

clean:
	rm -rf bin/
