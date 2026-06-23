# Build context must be the workspace root:
#   docker build -f cli/Dockerfile -t matching-engine/cli .
# Run `go work vendor` at the workspace root before building.

FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY api/go.mod api/go.sum api/
COPY cli/go.mod cli/go.sum cli/
COPY common/go.mod common/go.sum common/
COPY core/go.mod core/go.sum core/
COPY db/go.mod db/go.sum db/
COPY vendor/ vendor/

COPY cli/ cli/
COPY common/ common/
COPY db/ db/

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/cli ./cli/cmd

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/cli /bin/cli
ENTRYPOINT ["/bin/cli"]
