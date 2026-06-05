---
paths: ["**/*.go"]
---

# Go — Code Organization

## Utility Functions Belong in `pkg/utils`

Business files (handlers, services, repositories) must not contain utility functions that could
be reused elsewhere. Before implementing a helper inside a business file, apply this test:

> "Could this function be useful in any other file in this project, now or in the future?"
> If yes — it belongs in `pkg/utils`, not here.

**Move to `pkg/utils` when the function:**
- Formats, parses, or transforms data (strings, dates, numbers, UUIDs)
- Validates input that is not specific to one domain entity
- Wraps standard library calls for convenience
- Could be copy-pasted into another service without modification

**Keep in the business file only when the function:**
- Is tightly coupled to a private type defined in that same file
- Encapsulates logic that is meaningless outside of that specific context
- Would require importing the business package to be used elsewhere, creating a circular dependency

Exceptions to this rule must be extremely rare. If in doubt, move it to `pkg/utils`.

## Placing Functions in `pkg/utils`

Before creating a new file in `pkg/utils`, check whether an existing file is the right home
for the function. Use this as a guide:

| Function type                        | Target file               |
|--------------------------------------|---------------------------|
| String manipulation / formatting     | `pkg/utils/strings.go`    |
| Date / time helpers                  | `pkg/utils/time.go`       |
| Numeric / financial calculations     | `pkg/utils/math.go`       |
| UUID generation / parsing            | `pkg/utils/uuid.go`       |
| HTTP / request helpers               | `pkg/utils/http.go`       |
| Validation (generic, non-domain)     | `pkg/utils/validation.go` |
| Anything that does not fit above     | `pkg/utils/<topic>.go`    |

If no existing file fits, create a new one named after the topic — not after the caller.
Never create `pkg/utils/user_utils.go` or `pkg/utils/order_helpers.go`; name by what it does,
not where it came from.

## Examples

```go
// ❌ incorrect — formatOrderID is a string utility living inside a business file
// file: service/order.go
func (s *OrderService) Submit(ctx context.Context, order Order) error {
    id := formatOrderID(order.ID) // utility defined below in the same file
    ...
}

func formatOrderID(id string) string {
    return strings.ToUpper("ORD-" + id)
}

// ✅ correct — moved to pkg/utils/strings.go
// file: pkg/utils/strings.go
func FormatOrderID(id string) string {
    return strings.ToUpper("ORD-" + id)
}

// file: service/order.go
func (s *OrderService) Submit(ctx context.Context, order Order) error {
    id := utils.FormatOrderID(order.ID)
    ...
}
```

```go
// ✅ exception — helper is tightly coupled to a private type in the same file, never reusable
// file: service/matching.go
type matchResult struct { filled, remaining int64 }

func (m matchResult) isFullyFilled() bool {
    return m.remaining == 0  // meaningless outside this file — keep it here
}
```