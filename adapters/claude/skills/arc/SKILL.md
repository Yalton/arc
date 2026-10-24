---
name: arc
description: Use before feature work or creating files in a repository with .arch/registry.yaml, to find which architecture concept owns the code and what its rules are.
---

# arc

The repository's architecture registry is `.arch/registry.yaml`. Each file
belongs to one concept, and each concept lists its rules, tables and env vars.

- `arc brief <paths>` before changing code: the owning concepts' summaries
  and rules, boundaries, active flags and pending removals. `arc brief` with
  no arguments shows the whole registry.
- `arc place <path>` before creating a file in a new directory. An unowned
  path is refused by the hook and fails `arc gate`; extend a listed concept
  or add one to the registry with a reason.
- New tables, env vars, flags or concepts need a registry entry in the same
  change. Replacing code means deleting the old code or adding a `removals`
  entry.
- After editing the registry: `arc render`, then `arc validate`.
- `arc gate --staged` runs the pre-commit checks locally.
