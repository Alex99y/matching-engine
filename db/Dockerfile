# Build context must be the workspace root:
#   docker build -f db/Dockerfile -t matching-engine/db .
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

COPY common/ common/
COPY db/ db/

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/db ./db/cmd


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# The binary uses `file://../migrations` resolved from its working directory.
# Layout: /app/bin/db (binary) + /app/migrations/ matches ../migrations.
COPY --from=builder /bin/db /app/bin/db
COPY db/migrations/ /app/migrations/
WORKDIR /app/bin
ENTRYPOINT ["/app/bin/db"]
