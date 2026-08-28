BINARY := e2e
CMD    := ./cmd

-include .env
export

.PHONY: build test test-e2e clean

build:
	go build -o bin/$(BINARY) $(CMD)

# The CI check: run the stack-free unit tests, then compile the `e2e`-tagged suite under
# tests/ without running it (that needs a live stack — see test-e2e).
test:
	go test ./...
	go test -tags e2e -run 'x^' ./...

# Full suite against a live api+core+db+rabbitmq stack. Configure with E2E_* env
# (defaults target local-deploy); see PLAN.md §5.
test-e2e:
	go test -tags e2e -count=1 ./...

clean:
	rm -rf bin/
