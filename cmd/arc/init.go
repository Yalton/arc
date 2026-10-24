package main

import (
	"flag"
	"io"
	"strings"

	"arc/internal/initcmd"
)

func runInit(out *output, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts initcmd.Options
	fs.BoolVar(&opts.Scan, "scan", false, "propose one draft concept per package directory")
	fs.BoolVar(&opts.Claude, "claude", false, "install the Claude Code hook adapter")
	fs.BoolVar(&opts.Codex, "codex", false, "install the Codex hook adapter")
	fs.Bool("opencode", false, "not implemented until a later milestone")
	fs.StringVar(&opts.CI, "ci", "", "write a CI workflow running arc gate: github or gitea")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "init: "+err.Error())
	}
	if fs.NArg() > 0 {
		return out.fail(exitUsage, "init: unexpected arguments")
	}
	unsupported := false
	fs.Visit(func(f *flag.Flag) { unsupported = unsupported || f.Name == "opencode" })
	if unsupported {
		return out.fail(exitUsage, "init: --opencode is not implemented yet")
	}
	root, err := repoRoot()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	res, err := initcmd.Run(root, opts)
	if err != nil {
		return out.fail(exitUsage, "init: "+err.Error())
	}
	var b strings.Builder
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
