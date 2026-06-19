# Build context must be the workspace root:
#   docker build -f api/Dockerfile -t matching-engine/api .
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

COPY api/ api/
COPY common/ common/
COPY core/ core/
COPY db/ db/

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/api ./api/cmd


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/api /bin/api
USER 65534:65534
ENTRYPOINT ["/bin/api"]
