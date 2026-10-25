# arc — v1 specification

`arc` keeps agent-built codebases from outgrowing their architecture. It is a
single deterministic CLI (Go, one static binary) that reads a per-repo
architecture registry and:

- tells an agent, cheaply and at the moment it matters, which concept owns the
  code it is touching (`brief`, `place`, harness hooks);
- fails commits and CI when new structure lands without a registry entry, when
  boundaries are crossed, when flags or scheduled removals expire, or when dead
  code or oversized code is added (`gate`);
- checks beads against the registry and enriches them when beads is installed
  (`triage`);
- produces a deterministic drift report for an architect agent (`audit`).

Background and rationale: `../AGENT_ARCHITECTURE_GUARDRAILS.md`.

## Principles

1. **No model calls.** arc never invokes an LLM or an agent CLI. Where judgment
   is needed (`audit --prompt`), arc prints a prompt with data embedded and the
   caller decides what runs it.
2. **Agent agnostic.** All logic lives in the binary. Harness integrations are
   thin adapters that call `arc hook <harness>`. Enforcement that every agent
   hits regardless of harness lives in git hooks and CI.
3. **One source of truth.** `.arch/registry.yaml` is authoritative.
   `ARCHITECTURE.md` contains a rendered section generated from it; `gate`
   fails if the rendered section is stale.
4. **Beads optional, first class when present.** arc talks to beads only
   through the `bd` CLI. If `bd` is missing or the repo has no workspace, bead
   features are skipped with a notice, never an error.
5. **Wrap, don't reimplement.** Dead-code and lint tools (`deadcode`,
   `staticcheck`, `knip`, `vulture`, …) are run as configured commands.
6. **Incremental adoption.** Checks that would flag pre-existing problems
   compare against the base ref or a baseline file, so a messy repo can adopt
   arc without fixing everything first.

## Enforcement layers

| Layer | Mechanism | Reaches |
|---|---|---|
| CI | `arc gate --base origin/main` | Every agent and human; authoritative |
| Pre-commit | `arc gate --staged` (installed by `arc init`) | Every local committer |
| Harness pre-write hook | `arc hook claude\|codex\|opencode` | Agents in supported harnesses; earliest feedback |
| Instructions | Snippet in `AGENTS.md` / `CLAUDE.md` | Everything that reads instructions |

A harness without hooks loses only early feedback, not enforcement.

## Repository files

```
.arch/
  registry.yaml          # source of truth
  baseline/              # accepted pre-existing findings, per check
    deadcode.txt
  cache/                 # gitignored: hook session state
ARCHITECTURE.md          # prose + <!-- arc:begin --> … <!-- arc:end --> rendered block
AGENTS.md / CLAUDE.md    # arc snippet between <!-- arc:begin/end --> markers
```

## Registry schema (`.arch/registry.yaml`)

```yaml
version: 1

system:
  name: vapula
  summary: |
    3-5 lines: what the system does and for whom.

non_goals:
  - id: no-web-ui
    text: No browser UI; operators use the CLI and Gitea.
    keywords: [web ui, dashboard, frontend, react]   # used by triage

ignore:                     # never subject to placement checks
  - "**/*_test.go"
  - "vendor/**"
  - "docs/**"

concepts:
  - id: goal
    name: Goal
    summary: The only representation of an operator goal.
    paths: ["internal/workgraph/**"]      # files this concept owns
    storage: [work_nodes]                 # tables / collections / buckets
    env: [WORKGRAPH_DB_URL]               # env var names owned
    aliases: [goal, objective, target]    # used by triage and brief lookup
    # why it exists; gate requires it on concepts a change adds:
    reason: Goals were split across three tables; one model ends that.
    rules:                                # short, agent-facing
      - Goals are work_nodes with kind=goal; no separate goals table.
    draft: false                          # true = proposed by arc init --scan

  - id: shared
    name: Shared utilities
    summary: Small helpers with no domain meaning.
    paths: ["internal/util/**"]
    rules: [Nothing domain-specific; if it names a concept it belongs there.]

boundaries:
  - id: writes-through-actions
    from: ["internal/api/**", "internal/hermes/**"]
    deny: ["internal/store/**"]
    reason: All writes go through internal/actions.

flags:
  - name: USE_NEW_SCHEDULER
    concept: scheduler
    purpose: Gate the v2 scheduler during rollout.
    expires: 2026-12-01
    bead: vap-123

removals:
  - id: old-queue
    what: internal/queue (v1 job queue)
    paths: ["internal/queue/**"]
    replaced_by: scheduler
    remove_by: 2026-11-15
    bead: vap-130

limits:
  file_lines: 1000
  function_lines: 150          # enforced for Go natively in v1

checks:                         # external tools, run by gate
  - id: deadcode
    command: ["deadcode", "./..."]
    baseline: true              # only findings beyond the baseline fail
  - id: staticcheck
    command: ["staticcheck", "./..."]
    baseline: true
```

Validation rules (`arc validate`, also run by `gate`):

- ids unique; every `concept` reference resolves;
- every glob in `paths`, `from`, `deny` matches at least one existing file,
  except `removals[].paths` after the removal is done (then the entry must be
  deleted, see gate);
- no file is owned by more than one concept (most specific glob does not win;
  overlap is an error, so ownership stays unambiguous);
- `expires` / `remove_by` are valid dates.

## Commands

All commands accept `--json`. Exit codes: `0` ok, `1` violations found, `2`
usage, configuration or environment error (including a failed `bd` call).

### `arc init [--scan] [--claude] [--codex] [--opencode] [--ci github|gitea|none] [--no-detect]`

Creates `.arch/registry.yaml` (skeleton), `.arch/.gitignore` for `cache/`,
the rendered block in `ARCHITECTURE.md`, the instruction snippet in
`AGENTS.md` (and `CLAUDE.md` if present), and the pre-commit hook (respecting
`core.hooksPath`). An existing pre-commit hook is kept: arc appends its own
block between `# --- BEGIN ARC ---` / `# --- END ARC ---` markers and
refreshes that block in place on rerun, so tools that manage their own
marker sections (e.g. beads) coexist. If the existing hook ends in an
unconditional `exec` or `exit`, the block could never run; arc then moves
the hook to `pre-commit.local` and installs a hook that runs it first.
Harness flags install the matching adapter. Idempotent.

Plain `arc init` sets up everything that applies: each of `--claude`,
`--codex`, `--opencode`, `--ci` and `--scan` that is not given on the command
line is detected, reading only the repository, `PATH` and git config:

- a harness when its project directory (`.claude/`, `.codex/`, `.opencode/`)
  exists or its CLI (`claude`, `codex`, `opencode`) is on `PATH`;
- the CI provider from the host of `git remote get-url origin`: `github.com`
  is `github`; GitLab (`gitlab.com`, `gitlab.*`) and `bitbucket.org` get no
  workflow; any other host is `gitea` (Gitea or Forgejo); no origin or a
  local path gets none;
- `--scan` only when init creates the registry and the repository has a file
  neither the skeleton's ignore list nor the built-in ignores cover.

Init prints one line per item before what it created, e.g. `detected claude:
.claude/ exists`, `not detected codex: no .codex/ and codex not on PATH`,
`flag ci: none (--ci none)`; `--json` lists them under `detected`. Only the
remote's host is printed, never its URL. A flag given explicitly wins over
detection for that item: `--claude=false` (likewise `--codex`, `--opencode`,
`--scan`) and `--ci none` opt out of one item, and `--no-detect` turns
detection off so only the given flags apply. When the
CI provider was detected rather than requested, an existing workflow file
without arc's marker is left alone with a notice instead of failing init.

`--ci github|gitea` writes `.github/workflows/arc.yml` or
`.gitea/workflows/arc.yml` (Forgejo reads `.gitea/` too): `arc gate --base
origin/<target>` on pull requests and `arc gate --all` on pushes to the
default branch (`origin/HEAD`, else `main`), checking out with full history.
arc's module path is not fetchable, so the workflow builds arc from source:
it clones the git URL in the `ARC_REPO` repository variable at `ARC_REF`
(branch or tag, default `main`) and fails with a clear error while
`ARC_REPO` is unset; init prints a notice saying so. The file's first line
is an arc marker comment: rerun refreshes a marked file and refuses to touch
an unmarked one.

`--scan` proposes one `draft: true` concept per top-level package directory,
with `paths` filled in. Drafts are meant to be refined by a human or an
architect agent; `gate` warns while drafts exist.

### `arc place <path>...`

For each path: the owning concept, or exit 1 with "unowned" and the three
closest concepts (by shared path prefix, then alias match on path segments).
Ignored paths report `ignored`. Besides the registry's `ignore` list, arc
always ignores the files it manages or installs: `.arch/**`,
`ARCHITECTURE.md`, the root `AGENTS.md` and `CLAUDE.md`, and the harness
adapter directories `.claude/**`, `.codex/**` and `.opencode/**`.

### `arc brief [<path>... | --bead <id> | --concept <id>]`

Prints the agent-facing context for the given scope, sized to stay small
(target under 1,500 tokens for one concept):

- system summary (one line), relevant non-goals;
- each owning concept: summary, rules, storage, env;
- boundaries whose `from` matches the paths;
- active flags and pending removals touching those concepts.

With no arguments, prints the whole registry in compact form (the
"always-loaded" view, target under 4,000 tokens; warns if exceeded).
`--bead` resolves concepts from the bead's text via aliases and paths.

### `arc gate [--base <ref> | --staged | --all] [--only <check>...]`

Runs deterministic checks against the change set (diff vs base, or staged).
With no mode flag it judges uncommitted work against `HEAD`: staged and
unstaged changes plus untracked, non-ignored files. The pre-commit hook uses
`--staged`; CI uses `--base`. Gate takes no path arguments (exit 2 names
them and points to `place`/`brief`); giving two mode flags is also exit 2.

| Check | Fails when |
|---|---|
| `registry` | registry invalid, or rendered `ARCHITECTURE.md` block stale |
| `placement` | an added non-ignored file is owned by no concept |
| `structure` | the diff adds a `CREATE TABLE` name not in any `storage`, or reads a new env var name (Go `os.Getenv`/`LookupEnv`, Python `os.environ`/`getenv`, JS `process.env`) not in any `env` or `flags` |
| `boundaries` | an import in a changed file crosses a `deny` boundary (Go via `go list -json`; Python and TS/JS via import-line parsing) |
| `flags` | a flag is past `expires` |
| `removals` | a removal is past `remove_by`; or its paths no longer exist but the entry remains (delete the entry) |
| `size` | a changed file exceeds `file_lines`, or a changed Go function exceeds `function_lines` |
| `checks.*` | an external command reports a finding not in its baseline (compared as a multiset: a finding fails when its normalized form occurs more times than in the baseline) |
| `reasons` | the change adds a non-draft concept (or turns a draft into one) without a `reason`; skipped when the comparison point has no registry (adoption); with `--all`, every non-draft concept needs one |
| `drafts` | warning only: draft concepts exist |

Pre-existing violations in untouched files do not fail `placement`, `size`
or `boundaries`; only the change set is judged. `arc gate --all` judges the
whole tree (used by `audit`).

Every failure message names the registry entry involved and the fix, e.g.
`internal/foo/x.go: unowned. Closest: goal (internal/workgraph), shared.
Extend one, or add a concept to .arch/registry.yaml with a reason.`

### `arc baseline <check-id>`

Records the current findings of an external check as accepted. The baseline
file holds one normalized line per finding (line and column numbers
stripped), sorted, duplicates kept. Any output is accepted, since linters
exit non-zero with findings, so the command reports added/removed counts and
prints up to 5 newly accepted lines (`added_sample` in `--json`) for a human
to check that the check is configured right.

### `arc render`

Rewrites the rendered block in `ARCHITECTURE.md` and the snippet in
`AGENTS.md`/`CLAUDE.md`. When the repository uses beads (a `.beads/`
directory at the root), the snippet gains a line telling agents to give new
beads the `triage` template fields, with `Concepts touched` listing registry
concept ids separated by commas. Output depends only on the tree, so `gate`
staleness checks agree with `render`.

### `arc hook <claude|codex|opencode> [<event>]`

Reads the harness's hook payload on stdin, answers in the harness's format.

- **Before a file write/edit:** if the target is unowned and new, deny with
  the `place` message. Otherwise allow and attach `brief <path>` as added
  context, once per concept per session (session id from the payload; state
  in `.arch/cache/`).
- **After a shell command that ran `bd create`:** run `triage` on the new
  bead and return its result as added context. `bd create` must be a
  command (start of the line or after an unquoted `;`, `&`, `|`, newline or
  parenthesis), not text inside quotes or arguments. Only the bead ids the
  command's output names are triaged (`✓ Created issue: <id> — <title>`,
  `--json` objects with `"id"`, or the bare `--silent` id); with no id in
  the output nothing is triaged.
- Any internal error allows the action and prints a warning; a broken arc
  install must never block an agent.

Exact payload formats are pinned per harness at implementation time and
covered by fixture tests (Claude Code `PreToolUse`/`PostToolUse`; Codex hooks
— `codex features list` reports `hooks` stable; opencode v2 plugin
`ctx.tool.hook("execute.before"/"execute.after")` via a small JS shim in
`adapters/opencode`, installed as `.opencode/plugins/arc.js`).

### `arc triage [<bead-id>... | --new]` (beads only)

For each bead (`--new`: open beads without an `arch:*` label):

1. Template fields present: `Replaces / removes`, `Concepts touched`,
   `Closest existing thing`.
2. Concepts resolved from the text (aliases, paths); the brief is appended as
   a bead note.
3. Non-goal keywords matched.
4. Labels: `arch:ok` if fields are present, at least one concept resolved and
   no non-goal matched; otherwise `arch:review` with a note giving the
   reasons. Beads that name a concept not in the registry get `arch:review`,
   with a reason saying to list registry concept ids separated by commas.
   Applying one of `arch:ok` and `arch:review` removes the other if present
   (a bead fixed after review ends up with only `arch:ok`); triage touches no
   other labels.

`--dry-run` prints without writing. Re-triage skips the note when the bead's
most recent arc triage note has the same label and reasons (the brief in it
changes with the date). arc never closes, reprioritizes or deletes beads.

### `arc audit [--since 7d] [--prompt] [--file-beads]`

Deterministic drift report:

- non-test LOC per concept, now vs `--since` (from git history);
- lines added vs deleted; `fix:` vs `feat:` commit ratio;
- hotspot files (most fix commits);
- unowned files; draft concepts; non-draft concepts without a `reason`;
  concepts owning several tables;
- expired flags, overdue removals, removals whose bead is closed but paths
  remain;
- external check findings, including baselined ones (with counts).

`--prompt` prints the architect prompt with the report embedded, for any agent
to run. `--file-beads` (beads only) creates one bead per expired flag and
overdue removal, labelled `arch:consolidation`, deduplicated by an
`arc:<entry-id>` label on any bead, open or closed, so closed work is not
filed again.

### `arc validate`

Registry validation only.

## Beads integration details

- Detection: `bd` on `PATH` and `bd where` succeeds.
- All access via `bd show --json`, `bd list --json`, `bd label add`/`bd label remove`,
  `bd note`/`bd update`, `bd create`. No direct database access.
- Labels used: `arch:ok`, `arch:review`, `arch:consolidation`,
  `arc:<entry-id>`.
- `brief --bead`, `triage` and `audit` (removal bead status, and
  `--file-beads` deduplication) are the only commands that read beads;
  `audit --file-beads` and `triage` are the only ones that write.

## Code layout (arc's own)

```
cmd/arc/            main, flag parsing
internal/registry/  schema, load, validate, glob ownership, render
internal/gitx/      diff, staged set, history stats
internal/gate/      one file per check
internal/imports/   import extraction: go, python, ts/js
internal/brief/     brief and place
internal/initcmd/   arc init: scaffold, --scan, hooks, adapters
internal/hook/      harness payload adapters (claude, codex, opencode)
internal/beads/     bd CLI wrapper
internal/audit/     report, prompt template
adapters/claude/    plugin: hook config + short skill
adapters/opencode/  JS plugin shim
testdata/           fixture repos and hook payloads
```

arc dogfoods itself: its repo has its own `.arch/registry.yaml` and runs
`arc gate` in CI.

## Milestones

- **M1 — usable on a pilot:** registry load/validate/render, `place`,
  `brief`, `gate` (`registry`, `placement`, `structure`, `flags`, `removals`,
  `size`), `init` (incl. `--scan`, pre-commit, snippet), Claude hook adapter.
- **M2:** `boundaries`, external `checks` with baselines, beads `triage` and
  `brief --bead`, Codex adapter.
- **M3:** `audit` (+ `--prompt`, `--file-beads`), opencode adapter, CI
  snippets.

The pilot starts after M1: `arc init --scan`, an architect agent refines the
drafts using `audit --prompt` (or a manual equivalent until M3), then agents
work normally for a couple of weeks while we watch what `gate` and the hook
catch.

## Out of scope for v1

- Any LLM or agent invocation.
- Custom dead-code or call-graph analysis.
- A daemon, server, or web UI.
- Syncing beads to remote trackers.
- Function-length limits for languages other than Go.
