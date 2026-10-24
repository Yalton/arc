package initcmd

import (
	"strconv"
	"strings"
)

// defaultIgnore is the skeleton's ignore list; --scan applies it too.
var defaultIgnore = []string{
	// tests and fixtures
	"**/*_test.go",
	"**/testdata/**",
	"**/test_*.py",
	"**/*_test.py",
	"**/conftest.py",
	"**/*.test.ts",
	"**/*.spec.ts",
	"**/*.test.tsx",
	"**/*.spec.tsx",
	"**/*.test.js",
	"**/*.spec.js",
	"**/__tests__/**",
	// third-party and generated
	"vendor/**",
	"node_modules/**",
	"**/node_modules/**",
	"**/__pycache__/**",
	// docs and repository metadata
	"docs/**",
	"*.md",
	"README*",
	"LICENSE*",
	".arch/**",
	".beads/**",
	".claude/**",
	".opencode/**",
	".github/**",
	".gitea/**",
	".forgejo/**",
	".gitignore",
	".gitattributes",
	// build manifests
	"go.mod",
	"go.sum",
}

// skeleton returns the registry YAML for a fresh repository. It is written
// as text, not marshalled, so the comments survive for human editors.
func skeleton(name string, concepts []draft) string {
	var b strings.Builder
	b.WriteString(`# arc architecture registry (SPEC.md, "Registry schema").
# Source of truth for concepts, boundaries, flags and removals. After editing,
# run 'arc validate' and 'arc render'.
version: 1

system:
  name: ` + strconv.Quote(name) + `
  summary: |
    TODO: 3-5 lines on what the system does and for whom.

# Things this system deliberately does not do; triage matches keywords.
#   - id: no-web-ui
#     text: No browser UI; operators use the CLI.
#     keywords: [web ui, dashboard, frontend]
non_goals: []

# Files never subject to placement checks.
ignore:
`)
	for _, p := range defaultIgnore {
		b.WriteString("  - " + strconv.Quote(p) + "\n")
	}
	b.WriteString(`
# One entry per domain concept. Each file may be owned by at most one concept,
# and every glob in paths must match at least one file.
#   - id: goal
#     name: Goal
#     summary: The only representation of an operator goal.
#     paths: ["internal/goal/**"]
#     storage: [goals]          # tables / collections / buckets
#     env: [GOAL_DB_URL]        # env var names owned
#     aliases: [objective]
#     reason: Why it exists and why no existing concept could own it.
#     rules:
#       - Short, agent-facing rule.
`)
	if len(concepts) == 0 {
		b.WriteString("concepts: []\n")
	} else {
		b.WriteString("# Drafts proposed by 'arc init --scan': refine, then drop draft: true.\nconcepts:\n")
		for _, c := range concepts {
			b.WriteString("  - id: " + strconv.Quote(c.id) + "\n")
			b.WriteString("    name: " + strconv.Quote(c.name) + "\n")
			b.WriteString("    summary: TODO\n")
			b.WriteString("    paths: [" + strconv.Quote(c.path) + "]\n")
			b.WriteString("    draft: true\n")
		}
	}
	b.WriteString(`
# Import restrictions: files matching from may not depend on files matching deny.
#   - id: writes-through-actions
#     from: ["internal/api/**"]
#     deny: ["internal/store/**"]
#     reason: All writes go through internal/actions.
boundaries: []

# Feature flags; each names its owning concept and an expiry date.
#   - name: USE_NEW_SCHEDULER
#     concept: scheduler
#     purpose: Gate the v2 scheduler during rollout.
#     expires: 2026-12-01
flags: []

# Code scheduled for deletion.
#   - id: old-queue
#     what: internal/queue (v1 job queue)
#     paths: ["internal/queue/**"]
#     replaced_by: scheduler
#     remove_by: 2026-11-15
removals: []

limits:
  file_lines: 1000
  function_lines: 150

# External tools run by 'arc gate'.
#   - id: staticcheck
#     command: ["staticcheck", "./..."]
#     baseline: true
checks: []
`)
	return b.String()
}
