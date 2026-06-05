---
name: go-code-review
description: >
  Perform a structured code review of Go source files. Use this skill whenever the user asks
  Claude to review, inspect, audit, or "take a look at" any Go (.go) file or snippet — even
  if they just say "check this file" or "any issues with my code?". Covers security, performance,
  Go best practices, and basic linting. Ideal for iterative development workflows where the user
  wants quick, actionable feedback after each change. Trigger on phrases like: "review my Go code",
  "look at this file", "any issues?", "check for bugs", "audit my Go", or whenever a .go file is
  uploaded or pasted.
---

# Go Code Review Skill

Perform a focused, actionable code review on Go source files. The goal is fast, high-signal
feedback — not nitpicking. Think "senior Go engineer doing a PR review", not a linter report.

---

## Scope & Depth Limit

**Review only what the user explicitly provides. Do not follow imports.**

- **Depth 0 (default):** Review only the file(s) or snippet provided. Treat all imports as black boxes — do not open, read, or follow them.
- **Depth 1 (if user asks):** You may read direct imports that belong to the same local module (same repo), but only if the user says something like *"include dependencies"* or *"check the files it imports too"*.
- **Depth 2 (max, explicit only):** Only if the user explicitly requests full transitive analysis. Never go deeper than 2 levels.

When an import is relevant to a finding (e.g. a suspicious function call), **name it and note the concern** without reading the file:
> ⚠️ `order.Submit()` in `internal/order/order.go` — not reviewed (out of scope). Verify it handles X.

This keeps reviews fast and focused.

---

## Review Structure

Always produce the review in **four sections**. Keep each section tight: only report real issues,
skip sections that are clean, and group related findings.

---

### 1. 🔒 Security

Look for:
- **Injection risks**: SQL, shell, template injection via `fmt.Sprintf` into queries or commands
- **Unvalidated input**: missing bounds checks, unchecked type assertions (`x.(T)` without `ok`)
- **Sensitive data exposure**: secrets/tokens in logs, error messages, or struct tags
- **Crypto misuse**: `math/rand` instead of `crypto/rand` for sensitive values; weak hash functions (MD5, SHA1) for security purposes
- **Race conditions**: shared state accessed across goroutines without proper synchronization
- **Path traversal**: user-controlled paths passed to `os.Open`, `os.ReadFile`, etc. without sanitization
- **Unchecked errors**: silently discarded errors (`_`) on security-relevant calls (auth, file ops, network)

Severity labels: 🔴 Critical · 🟠 High · 🟡 Medium · 🔵 Info

---

### 2. ⚡ Performance

Look for:
- **Memory allocations in hot paths**: unnecessary `append` growth, string concatenation in loops (use `strings.Builder`)
- **Goroutine leaks**: goroutines started without a clear exit path or context cancellation
- **Unbounded concurrency**: spinning goroutines per request without a worker pool or semaphore
- **Inefficient data structures**: linear scans on large slices where a map would be O(1)
- **Repeated work**: expensive operations (marshal, compile, connect) inside loops that could be cached
- **Slice/map pre-allocation**: missing `make([]T, 0, n)` or `make(map[K]V, n)` when size is known
- **Context propagation**: blocking calls (`http.Get`, DB queries) that don't accept or respect a `context.Context`
- **Defer in loops**: `defer` inside a loop defers until function return, not loop iteration — can cause resource exhaustion

Flag only when the impact is likely non-trivial.

---

### 3. 🏗️ Go Best Practices

Look for:
- **Error handling**: errors returned but not wrapped with context (`fmt.Errorf("...: %w", err)`); sentinel errors where `errors.As`/`errors.Is` would be cleaner
- **Interface design**: interfaces defined at the consumer (not the implementor); overly large interfaces
- **Naming conventions**: unexported vs exported correctness; acronyms cased wrong (`URL` not `Url`, `ID` not `Id`)
- **Struct design**: exported fields that should be private; missing zero-value usability
- **init() abuse**: logic in `init()` that should be explicit; ordering dependencies
- **Package structure**: circular imports; exporting types only needed internally
- **Context as first arg**: functions that do I/O or blocking work should accept `ctx context.Context` as their first parameter
- **Panic vs error**: `panic` in library code (not main/test) where an error return would be appropriate
- **Test coverage signals**: exported functions with no test file referenced (note only, not a hard rule)

---

### 4. 🧹 Lint (Basic)

Apply a lightweight, non-pedantic pass. Flag only things that are clearly wrong or likely to cause bugs:
- Unused variables or imports (these are compile errors in Go, so flag if code wouldn't compile)
- `if err != nil { return err }` immediately after a call that could be a single-line — only flag if it meaningfully obscures logic
- Exported types/functions missing doc comments (flag once if pervasive, not per item)
- Dead code: unreachable branches, always-true conditions, functions never called
- `fmt.Println` / `log.Print` left in production paths (likely debug artifacts)
- Magic numbers: hardcoded values that should be named constants
- Shadowed variables: `:=` inside a block that shadows an outer variable of the same name

Skip style opinions (brace placement, line length, etc.) — `gofmt` handles those.

---

## Output Format

```
## Go Code Review — <filename or "snippet">

### 🔒 Security
<findings, or "No issues found.">

### ⚡ Performance
<findings, or "No issues found.">

### 🏗️ Go Best Practices
<findings, or "No issues found.">

### 🧹 Lint
<findings, or "No issues found.">

---
**Summary**: <1–2 sentence overall assessment. Call out the most important thing to fix.>
```

Each finding should follow this pattern:
```
**[Location]** — Brief description of the issue.
> Suggested fix or alternative approach.
```

Location can be a line number, function name, or struct name. Prefer function names when lines aren't available.

---

## Calibration Notes

- **Matching engine context**: this code likely has strict latency and correctness requirements.
  Pay extra attention to: lock contention, allocation in order-processing paths, and numeric
  precision issues (float vs int for prices/quantities).
- **Be direct**: skip compliments ("great use of interfaces!"). Report issues and move on.
- **Be proportional**: a 50-line file shouldn't get a 5-page review. Scale depth to file size and complexity.
- **Don't repeat yourself**: if the same pattern appears 10 times, flag it once with "(and N other occurrences)".
- **Actionable only**: if you can't suggest a concrete fix or alternative, don't flag it.