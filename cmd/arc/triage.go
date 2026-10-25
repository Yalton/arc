package main

import (
	"flag"
	"io"
	"strings"
	"time"

	"github.com/Yalton/arc/internal/beads"
)

// triageReport is the --json output of arc triage.
type triageReport struct {
	DryRun  bool           `json:"dry_run"`
	Notice  string         `json:"notice,omitempty"`
	Results []beads.Result `json:"results"`
}

// runTriage checks beads against the registry (SPEC.md, "arc triage").
// Review outcomes are reports, not failures: it exits 0 unless arguments
// are wrong or bd fails.
func runTriage(out *output, args []string) int {
	fs := flag.NewFlagSet("triage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	newOnly := fs.Bool("new", false, "triage open beads without an arch:* label")
	dryRun := fs.Bool("dry-run", false, "print without writing notes or labels")
	ids, err := parseInterspersed(fs, args)
	if err != nil {
		return out.fail(exitUsage, "triage: "+err.Error())
	}
	if *newOnly == (len(ids) > 0) {
		return out.fail(exitUsage, "triage: give bead ids or --new")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	rep := triageReport{DryRun: *dryRun, Results: []beads.Result{}}
	c, notice := beads.Open(r.root)
	if c == nil {
		rep.Notice = notice
		out.result(notice, rep)
		return exitOK
	}
	results, err := beads.Run(c, r.reg, ids, time.Time{}, time.Now().UTC(), *dryRun)
	if err != nil {
		return out.fail(exitUsage, "triage: "+err.Error())
	}
	code := exitOK
	var lines []string
	for _, res := range results {
		rep.Results = append(rep.Results, res)
		lines = append(lines, res.Summary())
		if res.Error != "" {
			code = exitUsage
		}
		if *dryRun && res.Note != "" {
			lines = append(lines, indent(res.Note))
		}
	}
	if len(results) == 0 {
		lines = append(lines, "triage: no untriaged open beads")
	}
	if *dryRun {
		lines = append(lines, "(dry run: no notes or labels written)")
	}
	out.result(strings.Join(lines, "\n"), rep)
	return code
}

// parseInterspersed parses flags that may appear before, between or after
// positional arguments, and returns the positional ones. A "--" ends flag
// parsing: everything after it is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if endedFlags(fs, args[:len(args)-fs.NArg()]) {
			return append(pos, fs.Args()...), nil
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		args = fs.Args()
		pos, args = append(pos, args[0]), args[1:]
	}
}

// endedFlags reports whether fs.Parse stopped at a "--" terminator, given
// the arguments it consumed: the last one is "--" and is not the value of
// a preceding non-boolean flag.
func endedFlags(fs *flag.FlagSet, consumed []string) bool {
	n := len(consumed)
	if n == 0 || consumed[n-1] != "--" {
		return false
	}
	if n == 1 {
		return true
	}
	prev := strings.TrimLeft(consumed[n-2], "-")
	if prev == consumed[n-2] || strings.Contains(prev, "=") {
		return true // not a flag, or a flag carrying its own value
	}
	f := fs.Lookup(prev)
	if f == nil {
		return true
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
