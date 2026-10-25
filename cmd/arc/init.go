package main

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/Yalton/arc/internal/initcmd"
)

func runInit(out *output, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts initcmd.Options
	fs.BoolVar(&opts.Scan, "scan", false, "propose one draft concept per package directory")
	fs.BoolVar(&opts.Claude, "claude", false, "install the Claude Code hook adapter")
	fs.BoolVar(&opts.Codex, "codex", false, "install the Codex hook adapter")
	fs.BoolVar(&opts.Opencode, "opencode", false, "install the opencode plugin shim")
	fs.BoolVar(&opts.Beads, "beads", false, "set beads: true in a new registry (the repository uses beads)")
	fs.StringVar(&opts.CI, "ci", "", "write a CI workflow running arc gate: github, gitea or none")
	noDetect := fs.Bool("no-detect", false, "apply only the flags given; detect nothing")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "init: "+err.Error())
	}
	if fs.NArg() > 0 {
		return out.fail(exitUsage, "init: unexpected arguments")
	}
	root, err := repoRoot()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	var found []initcmd.Finding
	if !*noDetect {
		explicit := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
		opts, found = initcmd.Resolve(root, os.Getenv("PATH"), opts, explicit)
	}
	res, err := initcmd.Run(root, opts)
	if err != nil {
		return out.fail(exitUsage, "init: "+err.Error())
	}
	if found != nil {
		res.Detected = found
	}
	var b strings.Builder
	for _, f := range found {
		b.WriteString(findingLine(f) + "\n")
	}
	for _, p := range res.Created {
		b.WriteString("created " + p + "\n")
	}
	for _, p := range res.Updated {
		b.WriteString("updated " + p + "\n")
	}
	for _, n := range res.Notices {
		b.WriteString("note: " + n + "\n")
	}
	if len(res.Created)+len(res.Updated) == 0 {
		b.WriteString("init: nothing to do\n")
	}
	out.result(strings.TrimRight(b.String(), "\n"), res)
	return exitOK
}

// findingLine renders one detection result, e.g.
// "detected claude: .claude/ exists" or "flag ci: none (--ci none)".
func findingLine(f initcmd.Finding) string {
	switch {
	case f.Source == "flag":
		return "flag " + f.Item + ": " + f.Value + " (" + f.Reason + ")"
	case f.Value == "off" || f.Value == "none":
		return "not detected " + f.Item + ": " + f.Reason
	case f.Value == "on":
		return "detected " + f.Item + ": " + f.Reason
	}
	return "detected " + f.Item + " " + f.Value + ": " + f.Reason
}
