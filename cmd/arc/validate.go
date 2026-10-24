package main

import (
	"fmt"
	"strings"

	"arc/internal/registry"
)

func runValidate(out *output, args []string) int {
	if len(args) > 0 {
		return out.fail(exitUsage, "validate: takes no arguments")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	problems := r.reg.Validate(r.files)
	registry.SortProblems(problems)
	if problems == nil {
		problems = []registry.Problem{}
	}

	var b strings.Builder
	for _, p := range problems {
		fmt.Fprintf(&b, "%s: %s\n", registry.Path, p)
	}
	if len(problems) == 0 {
		b.WriteString(registry.Path + ": ok\n")
	} else {
		fmt.Fprintf(&b, "%d problem(s)", len(problems))
	}
	out.result(strings.TrimRight(b.String(), "\n"), map[string]any{
		"ok":       len(problems) == 0,
		"problems": problems,
	})
	if len(problems) > 0 {
		return exitViolations
	}
	return exitOK
}
