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
	fs.BoolVar(&opts.Opencode, "opencode", false, "install the opencode plugin shim")
	ci := fs.String("ci", "", "not implemented until a later milestone")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "init: "+err.Error())
	}
	if fs.NArg() > 0 {
		return out.fail(exitUsage, "init: unexpected arguments")
	}
	if *ci != "" {
		return out.fail(exitUsage, "init: --ci is not implemented yet")
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
