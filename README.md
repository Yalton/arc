# arc

arc keeps agent-built codebases from outgrowing their architecture. It reads a
small registry (`.arch/registry.yaml`) that says which concept owns which code,
then tells agents who owns what before they write, and blocks commits and CI
when new structure lands without an owner, boundaries are crossed, or flags
and scheduled removals expire. It is one deterministic Go binary and never
calls a model.

## Quick start

1. **Install arc.**

   ```sh
   go install github.com/Yalton/arc/cmd/arc@latest
   ```

   Or download a binary for your platform from the
   [releases page](https://github.com/Yalton/arc/releases) and put it on your
   `PATH`.

2. **Set up a repository.**

   ```sh
   cd your-repo
   arc init
   ```

   `arc init` detects what the repository uses and sets it up. It prints what
   it detected and why.

3. **Review and commit.** Open `.arch/registry.yaml`, refine the draft
   concepts `init` proposed (summary, rules, a `reason`, then remove
   `draft: true`), run `arc render`, and commit.

## What `arc init` sets up

- `.arch/registry.yaml`, with one draft concept per top-level package when the
  repository already has code.
- The rendered view in `ARCHITECTURE.md` and agent instructions in `AGENTS.md`
  (and `CLAUDE.md` if present).
- A git pre-commit hook that runs `arc gate --staged`. An existing hook is kept;
  arc adds its own marked block.
- Agent hooks for each harness it finds (its config directory exists or its
  CLI is on `PATH`):
  - Claude Code: `.claude/settings.json`
  - Codex: `.codex/hooks.json`
  - opencode: `.opencode/plugins/arc.js`
- `beads: true` in a new registry when the repository has a `.beads/`
  directory, so the agent instructions ask for the bead fields `arc triage`
  checks.
- A CI workflow from the `origin` remote: `.github/workflows/arc.yml` for
  GitHub, `.gitea/workflows/arc.yml` for Gitea or Forgejo. It installs the same
  arc version with `go install` and needs no secrets.
  On pull requests, structural registry changes (a new concept, a loosened
  boundary, a removed rule, a later deadline, wider concept paths) fail until
  a maintainer adds the `arch-approved` label, so an agent cannot approve its
  own architecture change.

Any item can be turned off or forced: `--claude=false`, `--codex=false`,
`--opencode=false`, `--scan=false`, `--beads=false`, `--ci none|github|gitea`. `--no-detect`
applies only the flags you pass.

## Everyday commands

| Command | What it does |
|---|---|
| `arc brief [<path>...]` | The concepts, rules and boundaries for some paths (or everything) |
| `arc place <path>...` | Which concept owns a path, or the closest ones if none does |
| `arc gate` | Judge uncommitted work; `--staged`, `--base <ref>` or `--all` for other scopes |
| `arc render` / `arc validate` | Regenerate the rendered views / check the registry |
| `arc baseline <check>` | Accept the current findings of an external check |
| `arc triage [--new]` | Check beads against the registry (when [beads](https://github.com/gastownhall/beads) is installed) |
| `arc audit [--prompt]` | Drift report, or a prompt for an architect agent |

Every command accepts `--json`. Exit codes: `0` ok, `1` violations, `2`
usage, configuration or environment error.

## Harness notes

- **Codex** runs repository hooks only in a trusted project, and asks you to
  trust each hook once through `/hooks`.
- **opencode** (v2) loads the plugin at startup; `arc` must be on the `PATH` of
  the opencode process. If it is missing, the plugin allows everything.
- A harness without hooks still gets the instructions, the pre-commit hook
  and CI; it loses only the early feedback.

## More

The full design, registry schema and check list are in [SPEC.md](SPEC.md).
