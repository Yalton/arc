package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/Yalton/arc/internal/beads"
	"github.com/Yalton/arc/internal/brief"
	"github.com/Yalton/arc/internal/gitx"
)

// relPaths converts command-line paths (relative to the working directory
// or absolute) to repo-relative, slash-separated paths.
func relPaths(root string, args []string) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		abs, err := filepath.Abs(a)
		if err != nil {
			return nil, err
		}
		rel, ok := gitx.Rel(root, abs)
		if !ok {
			return nil, fmt.Errorf("%s is outside the repository", a)
		}
		out[i] = rel
	}
	return out, nil
}

func runPlace(out *output, args []string) int {
	if len(args) == 0 {
		return out.fail(exitUsage, "place: need at least one path")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	paths, err := relPaths(r.root, args)
	if err != nil {
		return out.fail(exitUsage, "place: "+err.Error())
	}
	results := make([]brief.PlaceResult, len(paths))
	var lines []string
	code := exitOK
	for i, p := range paths {
		results[i] = brief.Place(r.reg, p)
		lines = append(lines, brief.PlaceMessage(results[i]))
		if results[i].Status == brief.StatusUnowned {
			code = exitViolations
		}
	}
	out.result(strings.Join(lines, "\n"), results)
	return code
}

func runBrief(out *output, args []string) int {
	fs := flag.NewFlagSet("brief", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var concepts stringList
	fs.Var(&concepts, "concept", "brief for these concept ids (repeatable)")
	bead := fs.String("bead", "", "brief for the concepts a bead refers to")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "brief: "+err.Error())
	}
	scopes := 0
	for _, on := range []bool{len(concepts) > 0, fs.NArg() > 0, *bead != ""} {
		if on {
			scopes++
		}
	}
	if scopes > 1 {
		return out.fail(exitUsage, "brief: give one of paths, --concept or --bead")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	now := time.Now().UTC()
	var b brief.Brief
	switch {
	case *bead != "":
		ids, err := beadConcepts(r, *bead)
		if err != nil {
			return out.fail(exitUsage, "brief: --bead: "+err.Error())
		}
		if len(ids) == 0 {
			fmt.Fprintf(out.stderr, "arc: warning: no concepts resolved from bead %s; name concept ids, aliases or paths in it\n", *bead)
		}
		if b, err = brief.ForConcepts(r.reg, ids, now); err != nil {
			return out.fail(exitUsage, "brief: "+err.Error())
		}
	case len(concepts) > 0:
		if b, err = brief.ForConcepts(r.reg, concepts, now); err != nil {
			return out.fail(exitUsage, "brief: "+err.Error())
		}
	case fs.NArg() > 0:
		paths, err := relPaths(r.root, fs.Args())
		if err != nil {
			return out.fail(exitUsage, "brief: "+err.Error())
		}
		b = brief.ForPaths(r.reg, paths, now)
	default:
		b = brief.Whole(r.reg, now)
		if n := b.Tokens(); n > brief.WholeTokenBudget {
			fmt.Fprintf(out.stderr, "arc: warning: registry brief is ~%d tokens (budget %d); tighten summaries and rules\n",
				n, brief.WholeTokenBudget)
		}
	}
	out.result(strings.TrimRight(b.Text(), "\n"), b)
	return exitOK
}

// beadConcepts resolves the concepts a bead refers to, as triage does.
func beadConcepts(r *repo, id string) ([]string, error) {
	c, notice := beads.Open(r.root)
	if c == nil {
		return nil, errors.New(notice)
	}
	b, err := c.Show(id)
	if err != nil {
		return nil, err
	}
	return beads.Concepts(r.reg, b), nil
}
