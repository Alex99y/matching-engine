---
paths: ["**/*.go"]
---

# Go — Error Handling

## Never Ignore Errors

Every function or method that returns an error must handle it explicitly.
Discarding errors with `_` is not allowed unless accompanied by a comment explaining why it is safe.

## Package-Level Error Variables

Declare all errors at the top of the file, immediately after the imports.
The only place where `errors.New()` is allowed inside a function body is within factory functions
(`NewHandler`, `NewService`, `NewRepository`, etc.).

```go
// correct
var (
    ErrUserNotFound   = errors.New("user not found")
    ErrDuplicateEmail = errors.New("email already registered")
)

// incorrect — do not instantiate errors inside regular functions
func GetUser(id string) error {
    return errors.New("user not found") // ❌
}
```

## Wrap Errors at Layer Boundaries

Use `fmt.Errorf("operation description: %w", err)` when returning an error across layers.
This preserves the error chain for `errors.Is` / `errors.As` without leaking internals upward.

```go
// correct
func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
    user, err := s.repo.GetUserByID(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err)
    }
    return user, nil
}
```

## No Panics in Library Code

If a function encounters an unrecoverable state, return an error.
`panic` is only acceptable in `main()` during startup validation.