# arc — v1 specification

`arc` keeps agent-built codebases from outgrowing their architecture. It is a
single deterministic CLI (Go, one static binary) that reads a per-repo
architecture registry and:

- tells an agent, cheaply and at the moment it matters, which concept owns the
  code it is touching and what already exists (`brief`, `place`, `find`,
  harness hooks);
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
| Harness hooks (before writes, after shell commands) | `arc hook claude\|codex\|opencode` | Agents in supported harnesses; earliest feedback |
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

beads: true                     # repo tracks work in beads (snippet bead line)
```

Validation rules (`arc validate`, also run by `gate`):

- ids unique; every `concept` reference resolves;
- every glob in `paths`, `from`, `deny` matches at least one existing file,
  except `removals[].paths` after the removal is done (then the entry must be
  deleted, see gate);
- no file is owned by more than one concept (most specific glob does not win;
  overlap is an error, so ownership stays unambiguous);
- `expires` / `remove_by` are valid dates;
- `beads`, when present, is a boolean (default `false`).

## Commands

All commands accept `--json`. Exit codes: `0` ok, `1` violations found, `2`
usage, configuration or environment error (including a failed `bd` call).

### `arc init [--scan] [--claude] [--codex] [--opencode] [--beads] [--dupl] [--jscpd] [--ci github|gitea|none] [--no-detect]`

Creates `.arch/registry.yaml` (skeleton), `.arch/.gitignore` for `cache/`,
the rendered block in `ARCHITECTURE.md`, the instruction snippet in
`AGENTS.md` (and `CLAUDE.md` if present), and the pre-commit hook (respecting
`core.hooksPath`). An existing pre-commit hook is kept: arc appends its own
block between `# --- BEGIN ARC ---` / `# --- END ARC ---` markers and
refreshes that block in place on rerun, so tools that manage their own
marker sections (e.g. beads) coexist. If the existing hook ends in an
unconditional `exec` or `exit`, the block could never run; arc then moves
the hook to `pre-commit.local` and installs a hook that runs it first.
Harness flags install the matching adapter: for Claude Code,
`.claude/settings.json` entries running `arc hook claude` on `PreToolUse`
(matcher `Write|Edit|MultiEdit|NotebookEdit`) and `PostToolUse` (matcher
`Bash`); for Codex, `.codex/hooks.json` entries running `arc hook codex` on
`PreToolUse` (`^apply_patch$`) and `PostToolUse` (`^Bash$`); for opencode,
the plugin shim, which forwards file-writing tools before they run and
shell tools after. Hook files are merged per event: an event that already
runs arc's command is left alone and a missing one is added, so rerunning
init on an older install adds the shell post-hook. Idempotent.

Plain `arc init` sets up everything that applies: each of `--claude`,
`--codex`, `--opencode`, `--ci`, `--scan`, `--beads`, `--dupl` and `--jscpd` that is not given on the command
line is detected, reading only the repository, `PATH` and git config:

- a harness when its project directory (`.claude/`, `.codex/`, `.opencode/`)
  exists or its CLI (`claude`, `codex`, `opencode`) is on `PATH`;
- the CI provider from the host of `git remote get-url origin`: `github.com`
  is `github`; GitLab (`gitlab.com`, `gitlab.*`) and `bitbucket.org` get no
  workflow; any other host is `gitea` (Gitea or Forgejo); no origin or a
  local path gets none;
- `--scan` only when init creates the registry and the repository has a file
  neither the skeleton's ignore list nor the built-in ignores cover;
- `--beads` (write `beads: true` in the new registry) only when init creates
  the registry and a `.beads/` directory exists. For an existing registry
  without `beads: true`, init prints a notice suggesting the field when
  `.beads/` exists (or `--beads` is given) and leaves the registry alone;
- the duplicate-code checks: `--dupl` when the repository has Go files and
  `dupl` is on `PATH`, `--jscpd` when `jscpd` is on `PATH`. A missing tool's
  line names its install command (`go install github.com/mibk/dupl@latest`,
  `npm install -g jscpd`).

Each enabled duplicate-code check is added to `checks` only when the
registry (new or existing) has no check with that id, as `{id: dupl,
command: ["dupl", "-plumbing", "."], baseline: true}` or `{id: jscpd,
command: ["jscpd", "--reporters", "xcode", "--no-colors", "--no-tips", "."],
baseline: true}`. The registry is edited as text, touching only the `checks`
lines, so comments survive: the items are appended to a block list,
replace a `checks: []` line, or start a new `checks:` key at the end. When
none of those applies (e.g. a non-empty flow list), or the edited file would
not parse to the same registry plus the new checks, init leaves the file
alone and prints the exact YAML to add. For each check it added, init then
records the baseline (`arc baseline <id>`), so only new duplicates fail.

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
origin/<target>` on pull requests (with `ARC_APPROVED` from the
`arch-approved` label, rerun on label changes; see `approval`) and `arc gate
--all` on pushes to the default branch (`origin/HEAD`, else `main`), checking out with full history.
The workflow installs arc with `actions/setup-go` and `go install
github.com/Yalton/arc/cmd/arc@<version>`, pinned to the version of the arc
that wrote it; when that arc is not a release build (`dev`, a
pseudo-version, `+dirty`) it installs `@latest` and init prints a notice.
Rerunning init with a newer arc moves the pin. While the GitHub repository
is private (temporary), `go install` needs `GOPRIVATE=github.com/Yalton/*`
and git credentials for it in the runner. The file's first line
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
- each owning concept: summary, rules, storage, env, and what it already
  provides: the exported symbols of its non-ignored, non-test files (Go
  exported funcs, types, consts, vars and methods as `Type.Method`; Python
  top-level `def`/`class` not starting with `_`; TS/JS `export`ed
  function, class, const/let/var, interface, type and enum), with the first
  sentence of each doc comment or docstring. Text lists up to 25 names per
  concept grouped by file, methods cut first, then `+N more`; `--json` has
  every symbol with kind, doc, file and line;
- boundaries whose `from` matches the paths;
- active flags and pending removals touching those concepts.

With no arguments, prints the whole registry in compact form (the
"always-loaded" view, target under 4,000 tokens; warns if exceeded). It
omits provides and ends with a line pointing to `arc brief <path>` and
`arc find` for them.
`--bead` resolves concepts from the bead's text via aliases and paths.

### `arc find <words>...`

Ranks concepts and exported symbols (the provides of `arc brief`) against
the words, so an agent checks for existing code before planning new code.
Deterministic: words are lowercased, split at camelCase and `snake_case`,
stopwords dropped and crudely stemmed (`s`/`es`/`ies`, `ing`, `ed`, a final
`e`), so `parsing labels` meets `parseLabel`. Each query word scores once,
at the best field it matches; an exact token match counts in full, a prefix
match (shorter token at least 4 letters) half:

| Target | Field | Weight |
|---|---|---|
| symbol | name | 6 |
| symbol | doc | 3 |
| symbol | file path segments | 2 |
| concept | id, name, aliases | 6 |
| concept | summary, reason, rules | 2 |

Prints up to 3 concepts by score (then registry order) with why they
matched (`label (alias)`), then up to 10 symbols by score, then shorter
name, file and line, as `file:line Name - doc (concept)`; symbols under a
third of the best score are dropped. Exit 0 with results, 1 when nothing
matches, 2 with no words.

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
| `checks.*` | an external command reports a finding not in its baseline (compared as a multiset: a finding fails when its normalized form occurs more times than in the baseline); for the duplicate-code ids `dupl` and `jscpd` the message is `duplicates existing code: <line>; extend or reuse the existing code instead of copying` |
| `reasons` | the change adds a non-draft concept (or turns a draft into one) without a `reason`; skipped when the comparison point has no registry (adoption); with `--all`, every non-draft concept needs one |
| `approval` | `--base` only: the registry change is structural (below) and the pull request lacks the `arch-approved` label |
| `drafts` | warning only: draft concepts exist |

`approval` compares the registry at the merge base with the head and fails
each structural change with `needs human approval: <change>. A maintainer
approves by adding the arch-approved label to the pull request.` Structural
changes: an added concept, or a draft promoted to a concept; a removed
boundary, or one whose `deny` no longer covers a file it covered or whose
`from` now covers a file it did not; a rule removed from a concept (also by
removing the concept); a flag's `expires` or a removal's `remove_by` moved
later; a concept whose `paths` now own a file they did not. Coverage is
judged on the files in the head tree. The check runs only with `--base` and
only when the base has a registry (adoption is not judged); pre-commit,
`--staged`, working and `--all` modes skip it, so local agents can still
propose such changes. With `ARC_APPROVED=1` it is skipped with an info line,
which is printed but counted as neither error nor warning. The CI workflows
set `ARC_APPROVED` through `env` from
`contains(github.event.pull_request.labels.*.name, 'arch-approved')` and
trigger on `pull_request` types `opened`, `synchronize`, `reopened`,
`labeled` and `unlabeled`, so adding or removing the label reruns the gate.
Gitea and Forgejo runners evaluate expressions with act, which supports
`contains()` and the `.*` object filter, and their `pull_request` payload
carries `labels`; this is read from the runner source and docs, not tested
on a live instance.

Pre-existing violations in untouched files do not fail `placement`, `size`
or `boundaries`; only the change set is judged. `arc gate --all` judges the
whole tree (used by `audit`).

The checks with id `dupl` and `jscpd` are clone detectors. Their output is
canonicalized to one sorted line per clone, `<path>:<start>-<end>: duplicate
of <path>:<start>-<end>` (dupl `-plumbing` prints it; jscpd's xcode
reporter line `a.js:1:14: warning: Found 8 lines (1-9) duplicated on file
b.js (1-9)` is rewritten to it, and its `Found N clones.` and `time:`
summary lines are dropped). Formats verified with dupl (mibk/dupl, latest
as of 2026-10) and jscpd 5.4.2; other lines are kept as they are.

Every failure message names the registry entry involved and the fix, e.g.
`internal/foo/x.go: unowned. Closest: goal (internal/workgraph), shared.
Extend one, or add a concept to .arch/registry.yaml with a reason.`

### `arc baseline <check-id>`

Records the current findings of an external check as accepted. The baseline
file holds one normalized line per finding (line and column numbers, and
`<start>-<end>` line ranges, after a path stripped), sorted, duplicates kept. Any output is accepted, since linters
exit non-zero with findings, so the command reports added/removed counts and
prints up to 5 newly accepted lines (`added_sample` in `--json`) for a human
to check that the check is configured right.

### `arc render`

Rewrites the rendered block in `ARCHITECTURE.md` and the snippet in
`AGENTS.md`/`CLAUDE.md`. The snippet tells agents to run `arc brief` before
feature work and `arc find <what you need>` before adding a new file,
package or concept. When the registry sets `beads: true`, the snippet
gains a line telling agents to give new beads the `triage` template fields,
with `Concepts touched` listing registry concept ids separated by commas.
Output depends only on tracked files (the registry and the managed files),
never on untracked state such as a `.beads/` directory, so `gate` staleness
checks agree with `render` in a fresh clone (CI).

### `arc hook <claude|codex|opencode> [<event>]`

Reads the harness's hook payload on stdin, answers in the harness's format.

- **Before a file write/edit:** if the target is unowned and new, deny with
  the `place` message. Otherwise allow and attach `brief <path>` as added
  context, once per concept per session (session id from the payload; state
  in `.arch/cache/`). Creating a new file in an owned concept that was
  already briefed this session attaches that concept's provides again
  (a new file is where duplication starts).
- **After any shell command:** find the files it changed, with one
  `git --no-optional-locks status --porcelain=v1 -z --untracked-files=all
  --no-renames` call compared with the session's snapshot of dirty paths
  (porcelain code, modification time and size per path, kept in the same
  `.arch/cache/` state file as the briefed concepts). A dirty path whose
  signature differs from the snapshot counts as changed; on the session's
  first shell command, when there is no snapshot yet, only paths modified
  in the last 10 minutes count, so files that were dirty before the session
  are not blamed on it. Deleted paths and non-regular files are skipped.
  Of the changed files:
  - a new file (untracked or added to the index) that no concept owns gets
    feedback listing the `place` message of each (at most 10, then a count):
    move it into an existing concept's paths or register an owner. The
    command already ran, so this cannot deny; Claude Code gets it as the
    `PostToolUse` `{"decision": "block", "reason": ...}` feedback (the
    command's output is kept); Codex and opencode get it as added context,
    because Codex's `PostToolUse` block replaces the command's output with
    the reason and opencode's `execute.after` has no feedback channel. A
    pre-existing unowned file the command only modified is allowed, as
    before a write;
  - owned files get the same added context as a write: the brief of each
    concept not briefed this session, and a concept's provides again for a
    new file in an already briefed concept.

  Each new file is announced once per session, whether by a write hook or a
  shell hook. Claude Code runs `PostToolUse` only for tools
  that succeed and the opencode shim forwards only completed calls; files
  a failed command wrote are reported after the next command. Without a session id nothing persists, so every shell command
  is judged as a first one.
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
   When `Closest existing thing` is missing or `none`, the title and
   description are searched as `arc find` does; a symbol whose name has at
   least two words (for a method, ignoring the receiver type), all present
   in that text, is strong evidence, and up to 3 such symbols give
   `arch:review` with the reason `existing code may already cover this:
   <file:line Name>, ...`.
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
internal/symbols/   exported symbol extraction (go, python, ts/js) and find
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
