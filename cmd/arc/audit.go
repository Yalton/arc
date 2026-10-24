package main

import (
	"flag"
	"io"
	"strings"
	"time"

	"arc/internal/audit"
	"arc/internal/beads"
)

// auditOutput is the --json output of arc audit: the report, plus the
// filing outcome with --file-beads.
type auditOutput struct {
	*audit.Report
	FileBeads *fileBeadsOutput `json:"file_beads,omitempty"`
}

type fileBeadsOutput struct {
	Notice string        `json:"notice,omitempty"`
	Filed  []audit.Filed `json:"filed"`
}

// runAudit prints the deterministic drift report (SPEC.md, "arc audit"),
// or with --prompt the architect prompt around it. --file-beads also files
// a bead per expired flag and overdue removal. Findings are information,
// not failures: it exits 0 unless arguments, the registry, git or bd fail.
func runAudit(out *output, args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	since := fs.String("since", audit.DefaultSince, "history window: <N>d or <N>w")
	prompt := fs.Bool("prompt", false, "print the architect prompt with the report embedded")
	fileBeads := fs.Bool("file-beads", false, "file a bead per expired flag and overdue removal")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "audit: "+err.Error())
	}
	if fs.NArg() > 0 {
		return out.fail(exitUsage, "audit: unexpected argument "+fs.Arg(0))
	}
	if *prompt && *fileBeads {
		// The prompt is piped to an agent; filing output would pollute it.
		return out.fail(exitUsage, "audit: use --prompt or --file-beads, not both")
	}
	if _, err := audit.ParseSince(*since); err != nil {
		return out.fail(exitUsage, "audit: "+err.Error())
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	now := time.Now().UTC()
	opts := audit.Options{Root: r.root, Reg: r.reg, Files: r.files, Now: now, Since: *since}
	var bd *beads.Client
	notice := ""
	if *fileBeads || removalBeads(r) {
		bd, notice = beads.Open(r.root)
		opts.BeadsNotice = notice
		if bd != nil {
			opts.BeadStatus = func(id string) (string, error) {
				b, err := bd.Show(id)
				return b.Status, err
			}
		}
	}
	rep, err := audit.Build(opts)
	if err != nil {
		return out.fail(exitUsage, "audit: "+err.Error())
	}
	if *prompt {
		// arc never runs a model (SPEC.md, Principle 1); the caller does.
		text := audit.Prompt(rep, r.reg, now)
		out.result(text, map[string]string{"prompt": text})
		return exitOK
	}
	res := auditOutput{Report: rep}
	text := rep.Text()
	code := exitOK
	if *fileBeads {
		res.FileBeads = &fileBeadsOutput{Notice: notice, Filed: []audit.Filed{}}
		lines := []string{"", "File beads:"}
		if bd == nil {
			lines = append(lines, "  "+notice)
		} else {
			filed, err := audit.FileBeads(bd, audit.Proposals(rep))
			if err != nil {
				return out.fail(exitUsage, "audit: "+err.Error())
			}
			res.FileBeads.Filed = filed
			for _, f := range filed {
				lines = append(lines, "  "+f.Summary())
				if f.Error != "" {
					code = exitUsage
				}
			}
			if len(filed) == 0 {
				lines = append(lines, "  nothing to file (no expired flags or overdue removals)")
			}
		}
		text += "\n" + strings.Join(lines, "\n")
	}
	out.result(text, res)
	return code
}

// removalBeads reports whether any removal names a bead, so beads is only
// consulted when the report needs it.
func removalBeads(r *repo) bool {
	for _, rm := range r.reg.Removals {
		if rm.Bead != "" {
			return true
		}
	}
	return false
}
