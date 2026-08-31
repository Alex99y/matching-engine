MODULES := api cli core db loadtest

# Local development stack: dependency services (Postgres + RabbitMQ), migrations, and the
# default seed data. core and api are NOT started here — for development they run on the host
# (`make -C core run`, `make -C api run`) so a code change needs no image rebuild. They must
# start after this, since the API caches its market set at boot.
#
# The e2e suite runs everything in containers instead, including core and api; see
# e2e/docker-compose.yml and `make -C e2e stack-up`.
POSTGRESQL_URL ?= postgres://admin:admin@localhost:5432/matching-engine?sslmode=disable
COMPOSE_DEPS   := infra/local-deploy/docker-compose-deps.yml

.PHONY: build test clean stack-up stack-seed stack-down $(MODULES)

build:
	@for m in $(MODULES); do \
		echo "==> Building $$m"; \
		$(MAKE) -C $$m build; \
	done

test:
	@for m in $(MODULES); do \
		echo "==> Testing $$m"; \
		$(MAKE) -C $$m test; \
	done

clean:
	@for m in $(MODULES); do \
		echo "==> Cleaning $$m"; \
		$(MAKE) -C $$m clean; \
	done

stack-up:
	docker compose -f $(COMPOSE_DEPS) up -d --wait
	$(MAKE) -C db migrate POSTGRESQL_URL="$(POSTGRESQL_URL)"
	$(MAKE) stack-seed

# Shares one script with the e2e stack's seed service, so the default instruments and markets
# are defined in exactly one place.
stack-seed:
	$(MAKE) -C cli build
	CLI=./cli/bin/cli POSTGRESQL_URL="$(POSTGRESQL_URL)" sh infra/scripts/seed.sh

stack-down:
	docker compose -f $(COMPOSE_DEPS) down
