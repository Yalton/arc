package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// Exit codes shared by every command (SPEC.md, "Commands").
const (
	exitOK         = 0
	exitViolations = 1
	exitUsage      = 2
)

// output writes command results as text or, with --json, as one JSON value.
type output struct {
	json   bool
	stdout io.Writer
	stderr io.Writer
}

// result prints v as JSON in --json mode, otherwise prints text.
func (o *output) result(text string, v any) {
	if o.json {
		enc := json.NewEncoder(o.stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(v)
		return
	}
	fmt.Fprintln(o.stdout, text)
}

// fail reports an error and returns code. In --json mode the error goes to
// stdout as {"error": msg} so callers parse one stream.
func (o *output) fail(code int, msg string) int {
	if o.json {
		o.result("", map[string]string{"error": msg})
		return code
	}
	fmt.Fprintln(o.stderr, "arc: "+msg)
	return code
}
