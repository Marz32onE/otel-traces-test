---
name: clean-code-readability
description: Applies clean code principles to improve code quality and readability for humans (and AI agents). Use when writing or reviewing code, refactoring, or when the user asks about code quality, readability, clean code, or maintainability.
---

# Clean Code & Readability

Apply these principles so that **human junior developers and AI agents** can understand the code quickly. Prefer clarity and simplicity over cleverness.

## Core Principles

1. **Names** — Variables, functions, and types should reveal intent. Avoid abbreviations unless universal (e.g. `ctx`, `id`). Use one consistent term per concept in the file or package.
2. **Small units** — Functions do one thing; keep them short. Same for files: one clear responsibility per file when practical.
3. **Comments** — Explain *why* or non-obvious constraints, not *what*. Prefer self-explanatory names over comments. Keep package/doc comments accurate.
4. **DRY with care** — Remove real duplication (same logic in multiple places). Do **not** abstract "just in case"; if two call sites are the only users, duplication can be acceptable if it keeps each place obvious.
5. **No over-engineering** — If the code is already easy to follow, do not add layers, helpers, or patterns "for cleanliness." Optimize for "someone reads this file once and gets it."

## Quick Checklist (when writing or reviewing)

- [ ] Names are clear and consistent (no mixed terms for the same idea).
- [ ] Functions are focused and readable at a glance.
- [ ] Magic numbers have a reason (comment or named constant only when it helps; e.g. well-known ports can stay literal).
- [ ] Repeated logic is only extracted when it improves clarity or there are ≥2 real call sites.
- [ ] Comments add value (why / constraints), not noise.
- [ ] No unnecessary indirection or abstraction.

## When to Refactor vs Leave As-Is

| Refactor | Leave as-is |
|----------|-------------|
| Same expression/block in 3+ places | 2 similar blocks that are short and obvious in context |
| Long function that does several steps | Several short functions that are already easy to follow |
| Unclear or misleading name | Name is good enough to understand in 5 seconds |
| Comment that restates the code | No comment when the code is self-explanatory |

When in doubt, ask: "Would a junior dev or an AI agent understand this in one read?" If yes, prefer leaving it.

## Examples (tone, not rules)

**Naming**
- Prefer: `defaultPropagator`, `parseServerFromURI`, `ensureServiceNameAndVersion`.
- Avoid: `dp`, `parseURI` (if it only parses server part), `doStuff`.

**Duplication**
- Extract: The same 4-line block used in 3 places → one helper with a clear name.
- Keep: Two test files each with a small `findSpanByKind`-style helper if sharing would require a new package and the helpers are trivial; local duplication keeps each test file self-contained.

**Abstraction**
- Add: A shared `defaultPropagator` (or similar) when the same value is built in multiple places and the name documents intent.
- Skip: A "generic args parser" used only once; a single loop with type assertions is fine if it's easy to read.

## Output

When applying this skill during review or refactor:

- Suggest only changes that clearly improve readability or reduce confusion.
- State when something is "good enough" and refactor is optional or not recommended.
- Prefer one or two concrete edits over long lists; focus on high-impact, low-risk improvements.
