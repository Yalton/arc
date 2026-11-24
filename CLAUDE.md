# arc

Shared conventions (`base`, `agent-tool`), git authority, and the verify command are declared in `.fleet.json` and auto-loaded by the global `SessionStart` hook.

## Project

`arc` is a deterministic Go CLI that keeps agent-built codebases inside their architecture: a per-repo registry (`.arch/registry.yaml`), placement and brief lookups, a `gate` for pre-commit and CI, thin harness hook adapters (Claude, Codex, opencode), and optional beads integration through the `bd` CLI.

- `SPEC.md` is the design and the source of truth for behaviour. Read the sections relevant to your bead before starting. If a bead conflicts with the spec, stop and note it on the bead instead of improvising.
- Hard rule from the spec: `arc` never calls an LLM or agent CLI.
- Stay inside the code layout in `SPEC.md` ("Code layout"). Adding a package outside it needs a spec change first.

## Build & Test

```bash
go build ./...
go test ./...
go vet ./...
```

## Repo-Specific Rules

- Go module path: `github.com/Yalton/arc` (public on GitHub; Forgejo `origin` is the primary remote).
- Standard library first. Allowed dependencies: `gopkg.in/yaml.v3`, `github.com/bmatcuk/doublestar/v4` (globs). Anything else needs a note on the bead explaining why.
- Tests use fixture repos under `testdata/`, created in temp dirs with real `git` commands where diff behaviour matters.
- All commands support `--json`; exit codes `0` ok, `1` violations, `2` usage or config error.

<!-- arc:begin -->
## Architecture (arc)

The architecture registry is `.arch/registry.yaml`; `ARCHITECTURE.md` has the rendered view.

- Before feature work, run `arc brief <paths>` (or `arc brief` for everything) and follow the owning concept's rules.
- Extend the concept that owns the code; do not build a parallel version of something that exists.
- Before adding a new file, package or concept, run `arc find <what you need>` and extend what exists.
- New concepts, tables, env vars and flags need a registry entry with a reason in the same change.
- Replacing something means deleting the old code in the same change, or adding a `removals` entry.
- Before creating files in a new directory, run `arc place <path>`; unowned files fail `arc gate`.
- After editing the registry, run `arc render` and `arc validate`.
- When filing a bead, include `Replaces / removes:`, `Concepts touched:` (registry concept ids, comma-separated) and `Closest existing thing:` lines; `arc triage` checks them.
<!-- arc:end -->
