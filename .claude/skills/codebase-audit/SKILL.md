---
name: codebase-audit
description: >
  Perform a deep, critical audit of a single module directory as a senior engineer seeing it
  for the first time. Trigger this skill whenever the user asks to "audit", "deep review",
  "find real bugs", "what's actually wrong", or "be brutal" about a specific module or directory
  — even if they just say "take a hard look at api/" or "check what's broken in core/".
  This skill focuses exclusively on real defects: race conditions, silent error-swallowing,
  unsafe type coercions, dead code, and happy-path assumptions. It does NOT follow imports,
  refactor for taste, or add features. Output is always saved as a Markdown file in the
  `reviews/` folder, named by timestamp.
---

# Codebase Audit Skill

You are a senior engineer performing a first-time audit of a specific module directory.
Your job is to find what is **actually wrong** — not to suggest improvements, refactor for
style, or add features. Every finding must be a real defect with a real blast radius.

---

## Step 0 — Require a Scope Before Starting

**Always ask the user which module to audit before reading any file.** No exceptions.

The only valid scope is a module or directory the user explicitly names in their message
(e.g., "audit `core/`", "/codebase-audit api/"). Do NOT infer scope from:
- Files open in the IDE
- Recent conversation context
- Git status or recent commits
- Any other implicit signal

Ask exactly this, then stop and wait for the answer:

> "Which module should I audit? (e.g. `api/`, `core/`, `db/`) I'll read all `.go` files
> inside that directory only."

Do not audit multiple modules in one run unless the user explicitly lists them.
Do not proceed past Step 0 until the user has replied with a module name.

---

## Step 1 — Read the Module

Once scope is confirmed:

1. List all `.go` files directly inside the specified directory and its subdirectories.
2. Read every `.go` file found. Do not skip any file, including `_test.go` files.
3. Do not follow any import — local or external. Every imported package is a black box.

**Import handling rule:** If a finding depends on the behavior of an imported package
(local or external), flag it as unverifiable with the following note:

> ⚠️ Unverifiable without auditing `<package>` — flagged for awareness, not confirmed.

This keeps the audit honest about what was and wasn't read.

---

## Audit Categories

Investigate only these five categories. Skip everything else.

### 1. Concurrency & Race Conditions `[RACE]`
- Shared state accessed across goroutines without synchronization
- Goroutines started without a clear exit path or cancellation signal
- Channels that can deadlock or leak
- `sync.Mutex` / `sync.RWMutex` misuse (double-lock, unlock without lock)
- `sync/atomic` misuse or missing where needed
- Unbounded goroutine spawning (per-request, per-message, per-item loops)

### 2. Silent Error-Swallowing `[SILENT-ERROR]`
- Errors discarded with `_` on calls that can meaningfully fail
- `recover()` blocks that swallow panics without logging or re-raising
- Error returns ignored after goroutine launches
- Log-and-continue patterns that hide failures from callers
- Deferred calls whose errors are never checked (e.g., `defer rows.Close()`)

### 3. Unsafe Type Coercions & Casts `[UNSAFE-CAST]`
- Unchecked type assertions (`x.(T)` without the `ok` form)
- Integer overflow risks (int32 ↔ int64, uint ↔ int on sizes/lengths)
- Float used for financial or precision-sensitive values
- Implicit numeric truncation in assignments

### 4. Dead Code & Dead Config `[DEAD-CODE]`
- Config fields or env vars set but never consumed
- Functions defined and never called within the audited module
- Branches that can never be reached (always-true/false conditions)
- Commented-out code the rest of the system works around

### 5. Happy-Path Assumptions `[HAPPY-PATH]`
- Missing nil checks before pointer dereference
- Slice/map access without bounds or existence checks
- Assumptions about ordering that depend on map iteration (undefined in Go)
- External call results used without validating status or length
- Timeouts or context cancellation not propagated through I/O calls

---

## Finding Format

Every finding must follow this exact format. No vague findings allowed — if you cannot
cite a specific file and line, do not include the finding.

```
### [CATEGORY] Short description of the bug

**File:** `path/to/file.go`
**Line:** N (or range N–M)
**Why it's a bug:** One or two sentences explaining the defect precisely.
**Blast radius:** What breaks, how badly, and under what conditions.
**Smallest safe fix:** The minimal change that corrects the defect without altering behavior.
```

If a category has no findings, write exactly: `No issues found.` — do not manufacture
findings to fill the section.

---

## Output Structure

Structure the review document in this order:

### 1. Requires Approval
Changes that touch shared state, alter control flow, or have non-obvious side effects.
The engineer must review and approve each one before applying. **Maximum 10 findings.**
Order by blast radius — highest impact first.

### 2. Safe to Apply
Changes limited to: adding a missing error check, switching `x.(T)` to `x.(T, ok)`,
removing provably dead code, or adding a nil guard with no behavior change.
**Maximum 10 findings.** Order by blast radius — highest impact first.

### 3. Needs Separate Audit
List any imported local packages where findings were unverifiable due to the depth-0 rule.
One line per package. Format: `- \`internal/pkg/name\` — reason flagged.`
Maximum 5 entries. If none, omit this section entirely.

---

## Output Instructions

**Always write the review to a file. Never output it only in the chat.**

1. Create the `reviews/` directory at the project root if it does not exist.
2. Name the file using the current UTC timestamp and the module name: `reviews/YYYY-MM-DD_HH-MM-SS_audit_{MODULE_NAME_HERE}.md`
3. Write the full review to that file.
4. After writing, confirm in chat with exactly three lines:
   - File written to: `<path>`
   - Total findings: X requires approval, Y safe to apply
   - Most critical: `[CATEGORY]` one-line description

### Review File Header

```markdown
# Codebase Audit — <module name>
**Date:** YYYY-MM-DD HH:MM UTC
**Module audited:** `<directory/>`
**Files read:** N
**Imports followed:** None (depth-0 audit)
**Total findings:** N (X requires approval, Y safe to apply)

---
```

---

## Audit Conduct

- **Read everything first.** Read all files in scope before writing a single finding.
- **No taste opinions.** Do not flag naming, formatting, missing tests, or style unless
  they directly cause one of the five defect categories above.
- **Cite exactly.** File and line number required for every finding. No exceptions.
- **Proportional severity.** A race in the order-matching hot path outweighs a nil check
  in a one-time startup function. Severity determines order within each section.
- **Smallest safe fix only.** Do not rewrite. Do not refactor. Minimum change only.
- **When unsure, Requires Approval.** Never classify a change as Safe to Apply if you
  are not certain it is behavior-preserving.
- **Honest limitations.** Dynamic race conditions that only manifest under load are not
  reliably detectable by static analysis. Do not claim the audit is exhaustive.
