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

# Wire schema (common/proto → common/pkg/pb). Generated code is committed, so only a schema
# change needs this; protoc itself must already be on PATH. protoc-gen-go is pinned to the
# google.golang.org/protobuf version the modules depend on — generated code and runtime must match.
#
# No .proto names a Go import path. Each file's package follows from where it sits:
# proto/me/v1/x.proto → <module>/pkg/pb/me/v1, named mev1 (its directory without the slashes),
# with <module> read from common/go.mod. A new schema directory needs nothing here.
PROTOC_GEN_GO_VERSION := v1.36.8
PROTO_SRC             := common/proto
PROTO_GO_PKG          := pkg/pb
PROTO_FILES           := $(patsubst $(PROTO_SRC)/%,%,$(shell find $(PROTO_SRC) -name '*.proto' | sort))
PROTO_GO_MODULE        = $(shell cd common && GOWORK=off go list -m)
proto_dir              = $(patsubst %/,%,$(dir $(1)))
proto_go_package       = $(PROTO_GO_MODULE)/$(PROTO_GO_PKG)/$(call proto_dir,$(1));$(subst /,,$(call proto_dir,$(1)))
export PATH           := $(shell go env GOPATH)/bin:$(PATH)

.PHONY: build test clean proto stack-up stack-seed stack-down $(MODULES)

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

proto:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	protoc -I $(PROTO_SRC) --go_out=common/$(PROTO_GO_PKG) --go_opt=paths=source_relative \
		$(foreach f,$(PROTO_FILES),'--go_opt=M$(f)=$(call proto_go_package,$(f))') \
		$(addprefix $(PROTO_SRC)/,$(PROTO_FILES))

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
