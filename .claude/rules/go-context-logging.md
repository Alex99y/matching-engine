---
paths: ["**/*.go"]
---

# Go — Context & Logging

## Context Propagation

Every function that performs I/O or calls a downstream service must accept `context.Context`
as its first parameter, named `ctx`. This applies to handlers, services, and repositories alike.
Never store a context in a struct field.

```go
// correct
func (r *userRepository) GetUserByID(ctx context.Context, id string) (*User, error)

// incorrect — missing context
func (r *userRepository) GetUserByID(id string) (*User, error) // ❌
```

## Log Once, at the Origin

Log each error exactly once, at the layer where it originates or is first caught.
Do not re-log an error as it propagates up the call stack — this produces duplicate entries
and makes traces harder to read.

| Layer        | Logging responsibility                                        |
|--------------|---------------------------------------------------------------|
| `Repository` | Logs infrastructure errors before returning them wrapped      |
| `Service`    | Does not log errors received from the repository              |
| `Handler`    | Logs only errors that originate within the handler itself     |