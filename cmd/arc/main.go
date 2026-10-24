// Command arc keeps agent-built codebases inside their architecture.
// See SPEC.md for behaviour.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

var commands = []string{
	"init", "place", "brief", "gate", "baseline", "render",
	"hook", "triage", "audit", "validate", "version",
}

// handlers maps implemented commands to their entry points.
var handlers = map[string]func(*output, []string) int{
	"gate":     runGate,
	"brief":    runBrief,
	"hook":     runHook,
	"init":     runInit,
	"place":    runPlace,
	"render":   runRender,
	"validate": runValidate,
	"version":  runVersion,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes arc with args and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	out := &output{stdout: stdout, stderr: stderr}
	args, out.json = extractJSONFlag(args)

	if len(args) == 0 {
		fmt.Fprint(stderr, usage())
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage())
		return exitOK
	}
	if h, ok := handlers[cmd]; ok {
		return h(out, rest)
	}
	for _, c := range commands {
		if c == cmd {
			return out.fail(exitUsage, cmd+": not implemented")
		}
	}
	return out.fail(exitUsage, fmt.Sprintf("unknown command %q\n\n%s", cmd, usage()))
}

// extractJSONFlag removes --json from args (before any "--") so every
// command accepts it in any position.
func extractJSONFlag(args []string) ([]string, bool) {
	var kept []string
	found := false
	for i, a := range args {
		if a == "--" {
			kept = append(kept, args[i:]...)
			break
		}
		if a == "--json" || a == "-json" {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	return kept, found
}

func runVersion(out *output, args []string) int {
	if len(args) > 0 {
		return out.fail(exitUsage, "version: takes no arguments")
	}
	out.result("arc "+version, map[string]string{"version": version})
	return exitOK
}

func usage() string {
	return "usage: arc [--json] <command> [args]\n\ncommands: " +
		strings.Join(commands, ", ") + "\n"
}
