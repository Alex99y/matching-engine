---
paths: ["**/*.go"]
---

# Go — Layer Architecture

## Call Chain

The only permitted call direction is:

```
Handler → Service → Repository
```

A handler must never call a repository directly.
A service must never reference HTTP types (`http.Request`, `http.ResponseWriter`, status codes).

## Isolate Infrastructure Errors from the API

Infrastructure errors (database, message broker, cache) must be caught at the service layer,
logged once, and translated into a generic response. HTTP handlers must return
`500 Internal Server Error` with no internal detail in the response body.

## Wrap Dependencies Behind Interfaces

Any service that depends on a repository, external client, or infrastructure component must
declare a local interface with only the methods it needs. The interface is declared in the
**consumer package** (the service), not the provider package (the repository).
This keeps unit tests free of real I/O.

```go
// declared in the service package
type UserRepository interface {
    InsertUser(ctx context.Context, username, email, passwordHash string) error
    GetUserByUsername(ctx context.Context, username string) (*repository.User, error)
}
```

## Do Not Expose Internal IDs

Never expose internal database IDs (auto-increment integers, internal UUIDs) in API responses
or external-facing data structures. The only identifier safe to expose externally is `UserID`,
which is a UUIDv7 generated at creation time.

## Layer Responsibility Reference

| Layer        | Knows about                              | Must not know about              |
|--------------|------------------------------------------|----------------------------------|
| `Handler`    | HTTP request/response, Service interface | DB, RabbitMQ, internal errors    |
| `Service`    | Business logic, Repository interface     | HTTP, status codes, DB drivers   |
| `Repository` | DB / infrastructure                      | Business logic, HTTP, Service types |