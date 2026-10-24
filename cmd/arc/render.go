package main

import "strings"

func runRender(out *output, args []string) int {
	if len(args) > 0 {
		return out.fail(exitUsage, "render: takes no arguments")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	changed, err := r.reg.Render(r.root)
	if err != nil {
		return out.fail(exitUsage, "render: "+err.Error())
	}
	if changed == nil {
		changed = []string{}
	}
	text := "render: up to date"
	if len(changed) > 0 {
		text = "render: updated " + strings.Join(changed, ", ")
	}
	out.result(text, map[string]any{"changed": changed})
	return exitOK
}
