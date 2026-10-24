package main

import (
	"fmt"
	"os"
	"time"

	"arc/internal/hook"
)

// runHook answers a harness hook. It reads the payload from stdin and
// always exits 0 once the harness is known: a broken arc must never block
// an agent (SPEC.md, "arc hook").
func runHook(out *output, args []string) int {
	if len(args) > 0 && args[0] == "claude" {
		return hook.Claude(os.Stdin, out.stdout, out.stderr, time.Now().UTC())
	}
	// A misconfigured hook must not block the agent: report on stderr and
	// exit 0, since some harnesses treat exit 2 as a block.
	harness := "(none)"
	if len(args) > 0 {
		harness = args[0]
	}
	fmt.Fprintf(out.stderr, "arc hook: unsupported harness %s (supported: claude); allowing\n", harness)
	return exitOK
}
