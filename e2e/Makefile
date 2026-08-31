BINARY  := e2e
CMD     := ./cmd
COMPOSE := docker compose -f docker-compose.yml

-include .env
export

.PHONY: build test test-e2e test-e2e-v stack-up stack-down stack-logs clean

build:
	go build -o bin/$(BINARY) $(CMD)

# Run the stack-free unit tests, then compile the `e2e`-tagged suite under tests/ without
# running it (that needs a live stack — see test-e2e).
test:
	go test ./...
	go test -tags e2e -run 'x^' ./...

# Bring up everything the suite needs: Postgres, RabbitMQ, migrations, seed data, core, api.
# vendor/ is gitignored and the images build with -mod=vendor, so it has to exist first.
stack-up:
	cd .. && go work vendor
	$(COMPOSE) up -d --build --wait
	@echo "==> stack up; api on http://localhost:4000"

stack-down:
	$(COMPOSE) down -v

stack-logs:
	$(COMPOSE) logs --no-color --tail=200

# Full suite against a live stack. Configure with E2E_* env (defaults target localhost:4000);
# see PLAN.md §5.
#
# -p 1 runs the four suites one after another rather than concurrently: they share one order
# book, and several assert on market-wide state (last traded price, candle windows). Timeout
# is generous because the whole suite provisions ~30 funded accounts over HTTP.
test-e2e:
	go test -tags e2e -count=1 -p 1 -timeout 20m ./...

# Same, with each test's name and duration printed as it runs.
test-e2e-v:
	go test -tags e2e -count=1 -p 1 -timeout 20m -v ./...

clean:
	rm -rf bin/
