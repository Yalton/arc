package gate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"arc/internal/registry"
)

// External checks are the registry's `checks` entries (SPEC.md principle 5:
// wrap, don't reimplement). Each one appears in the gate as
// "checks.<id>"; "--only checks" selects them all.
const (
	ExternalPrefix = "checks."
	ExternalGroup  = "checks"
	BaselineDir    = ".arch/baseline"
)

// ExternalTimeout bounds one external command run.
var ExternalTimeout = 5 * time.Minute

// externalChecks returns one Check per registry check, in registry order.
func externalChecks(reg *registry.Registry) []Check {
	if reg == nil {
		return nil
	}
	out := make([]Check, 0, len(reg.Checks))
	for _, k := range reg.Checks {
		out = append(out, Check{ExternalPrefix + k.ID, func(ctx *Context) ([]Finding, error) {
			return checkExternal(ctx, k)
		}})
	}
	return out
}

// ExternalCheck returns the registry check with id ("deadcode" or
// "checks.deadcode"), or nil.
func ExternalCheck(reg *registry.Registry, id string) *registry.Check {
	id = strings.TrimPrefix(id, ExternalPrefix)
	for i := range reg.Checks {
		if reg.Checks[i].ID == id {
			return &reg.Checks[i]
		}
	}
	return nil
}

// checkExternal runs k on the whole tree, whatever the gate mode: external
// tools judge the repository, and baselines provide incremental adoption
// (SPEC.md principle 6). With baseline, only findings whose normalized form
// is not in the baseline file fail.
func checkExternal(ctx *Context, k registry.Check) ([]Finding, error) {
	entry := "checks[" + k.ID + "]"
	lines, err := RunExternal(ctx.Root, k)
	if err != nil {
		return []Finding{{Path: registry.Path, Entry: entry, Message: err.Error()}}, nil
	}
	accepted := map[string]bool{}
	suffix := ""
	if k.Baseline {
		path, err := BaselinePath(k.ID)
		if err != nil {
			return []Finding{{Path: registry.Path, Entry: entry, Message: err.Error()}}, nil
		}
		base, err := ReadBaseline(ctx.Root, k.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range base {
			accepted[b] = true
		}
		suffix = " (not in " + path + "; fix it)"
	}
	var out []Finding
	for _, l := range lines {
		if accepted[Normalize(l)] {
			continue
		}
		out = append(out, Finding{Entry: entry, Message: l + suffix})
	}
	return out, nil
}

// RunExternal runs k's command (argv, no shell) in root and returns the
// non-empty lines of its combined stdout and stderr. The returned error is
// a user-facing problem: command missing, timed out, or failed silently.
// A non-zero exit with output is not an error; the output is the findings.
func RunExternal(root string, k registry.Check) ([]string, error) {
	if len(k.Command) == 0 {
		return nil, fmt.Errorf("check %s has no command. Set checks[%s].command in %s.", k.ID, k.ID, registry.Path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ExternalTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, k.Command[0], k.Command[1:]...)
	cmd.Dir = root
	cmd.WaitDelay = 5 * time.Second
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("check %s: command %q not found. Install it, or remove checks[%s] from %s.",
			k.ID, k.Command[0], k.ID, registry.Path)
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("check %s timed out after %s", k.ID, ExternalTimeout)
	}
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("check %s: %v", k.ID, err)
	}
	if err != nil && len(lines) == 0 {
		return nil, fmt.Errorf("check %s failed: %v", k.ID, exitErr.ProcessState)
	}
	return lines, nil
}

// locRE matches "<path>:<line>" or "<path>:<line>:<col>", where the path
// token contains a '.' or '/' so plain "12:30" times are left alone.
var locRE = regexp.MustCompile(`([^\s:]*[./][^\s:]*):\d+(?::\d+)?\b`)

// Normalize returns the form of a finding compared against a baseline:
// line and column numbers after paths stripped and whitespace collapsed,
// so accepted findings survive edits that shift lines.
func Normalize(line string) string {
	return strings.Join(strings.Fields(locRE.ReplaceAllString(line, "$1")), " ")
}

var baselineIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// BaselinePath returns the repository-relative baseline file of check id.
func BaselinePath(id string) (string, error) {
	if !baselineIDRE.MatchString(id) || strings.Contains(id, "..") {
		return "", fmt.Errorf("check id %q cannot name a baseline file; use letters, digits, '.', '_' or '-' in checks[].id", id)
	}
	return BaselineDir + "/" + id + ".txt", nil
}

// ReadBaseline returns the normalized findings accepted for check id; a
// missing file accepts nothing.
func ReadBaseline(root, id string) ([]string, error) {
	path, err := BaselinePath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = Normalize(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// BaselineResult reports what RecordBaseline wrote.
type BaselineResult struct {
	Check    string `json:"check"`
	Path     string `json:"path"`
	Findings int    `json:"findings"`
	Added    int    `json:"added"`
	Removed  int    `json:"removed"`
}

// RecordBaseline runs k and writes its normalized findings, sorted and
// deduplicated, as the accepted baseline.
func RecordBaseline(root string, k registry.Check) (*BaselineResult, error) {
	path, err := BaselinePath(k.ID)
	if err != nil {
		return nil, err
	}
	lines, err := RunExternal(root, k)
	if err != nil {
		return nil, err
	}
	old, err := ReadBaseline(root, k.ID)
	if err != nil {
		return nil, err
	}
	now := map[string]bool{}
	for _, l := range lines {
		now[Normalize(l)] = true
	}
	was := map[string]bool{}
	for _, l := range old {
		was[l] = true
	}
	res := &BaselineResult{Check: ExternalPrefix + k.ID, Path: path, Findings: len(now)}
	sorted := make([]string, 0, len(now))
	for l := range now {
		sorted = append(sorted, l)
		if !was[l] {
			res.Added++
		}
	}
	for l := range was {
		if !now[l] {
			res.Removed++
		}
	}
	sort.Strings(sorted)
	content := strings.Join(sorted, "\n")
	if content != "" {
		content += "\n"
	}
	p := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}
