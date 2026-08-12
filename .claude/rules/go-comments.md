---
paths: ["**/*.go"]
---

# Go — Comment Minimalism

## Comment the Why, Not the What

Do not narrate what the code already says. A comment describing what the next line does adds
noise, not information — the reader can already see the code.

```go
// incorrect — restates the code ❌
// Increment the counter by one
counter++

// correct — no comment needed, the code is self-explanatory
counter++
```

Only write a comment when it explains something the code cannot: *why* a non-obvious choice was
made, a business rule being enforced, a workaround for an external constraint, or a concurrency/
performance assumption that would be unsafe to change.

```go
// correct — explains a non-obvious constraint the code alone doesn't convey
// Lock price before qty: matching engine convention, prevents lock-order
// inversion with the order book writer.
mu.priceLock.Lock()
mu.qtyLock.Lock()
```

## A Self-Explanatory Name Needs No Comment — Even If Exported

Do not add a doc comment just because an identifier is exported. If the name and signature
already say what it does, a comment is redundant and must be omitted entirely.

```go
// incorrect — the name already says exactly this ❌
// GetUserByID retrieves a user from the database given their unique identifier.
// It returns the user object if found, or an error if the user does not exist
// or if there was a problem communicating with the database.
func GetUserByID(ctx context.Context, id string) (*User, error)

// incorrect — still redundant, just shorter ❌
// GetUserByID returns the user with the given id.
func GetUserByID(ctx context.Context, id string) (*User, error)

// correct — no comment, the signature is the documentation
func GetUserByID(ctx context.Context, id string) (*User, error)
```

Write a doc comment only when the name cannot carry the behavior on its own: non-obvious error
conditions (`ErrUserNotFound` vs a generic error), side effects not implied by the name, or
concurrency/ordering requirements on the caller. Keep it to one sentence.

```go
// correct — the name alone doesn't tell the caller this can return a
// sentinel error they're expected to check with errors.Is
// GetUserByID returns ErrUserNotFound if no user exists with that id.
func GetUserByID(ctx context.Context, id string) (*User, error)
```

## No Step-by-Step Narration Inside Function Bodies

Do not add a comment above every block restating what that block does. If a function needs that
many comments to stay readable, split it into smaller, well-named functions instead of narrating
it with comments.

```go
// incorrect — comments substitute for clear structure ❌
func (s *OrderService) Submit(ctx context.Context, order Order) error {
    // validate the order
    if err := order.Validate(); err != nil {
        return err
    }
    // persist the order
    if err := s.repo.Insert(ctx, order); err != nil {
        return err
    }
    // publish the event
    return s.publisher.Publish(ctx, order.ToEvent())
}

// correct — structure alone makes intent clear, no comments needed
func (s *OrderService) Submit(ctx context.Context, order Order) error {
    if err := order.Validate(); err != nil {
        return err
    }
    if err := s.repo.Insert(ctx, order); err != nil {
        return err
    }
    return s.publisher.Publish(ctx, order.ToEvent())
}
```

## Default to Zero Comments

When generating Go code, the default is no comment on anything — exported or not. Add one only
when a future reader, with no extra context, would misunderstand the code or its failure modes
without it. When in doubt, leave it out.
</content>
