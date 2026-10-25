package initcmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/Yalton/arc/internal/gate"
	"github.com/Yalton/arc/internal/registry"
	"gopkg.in/yaml.v3"
)

// Clone detectors arc init can wire in as external checks (SPEC.md
// principle 5). Flags verified by running the tools: dupl (mibk/dupl,
// -plumbing prints "path:start-end: duplicate of path:start-end") and jscpd
// 5.4.2 (the xcode reporter prints one line per clone; gate canonicalizes
// it, internal/gate/clones.go).
var cloneChecks = map[string]registry.Check{
	gate.DuplID:  {ID: gate.DuplID, Command: []string{"dupl", "-plumbing", "."}, Baseline: true},
	gate.JscpdID: {ID: gate.JscpdID, Command: []string{"jscpd", "--reporters", "xcode", "--no-colors", "--no-tips", "."}, Baseline: true},
}

var cloneInstall = map[string]string{
	gate.DuplID:  "go install github.com/mibk/dupl@latest",
	gate.JscpdID: "npm install -g jscpd",
}

// detectClone reports a clone detector present when its binary is on
// pathList; dupl also needs Go files, the only language it reads.
func detectClone(root, pathList, id string) (bool, string) {
	if id == gate.DuplID {
		files, err := registry.RepoFiles(root)
		if err != nil {
			return false, "cannot list files: " + err.Error()
		}
		hasGo := false
		for _, f := range files {
			hasGo = hasGo || strings.HasSuffix(f, ".go")
		}
		if !hasGo {
			return false, "no Go files"
		}
	}
	if p := lookPath(pathList, id); p != "" {
		return true, id + " on PATH (" + p + ")"
	}
	return false, id + " not on PATH; install it with '" + cloneInstall[id] + "' and rerun arc init"
}

// wantedClones lists the clone checks opts asks for, in a fixed order.
func wantedClones(opts Options) []registry.Check {
	var out []registry.Check
	if opts.Dupl {
		out = append(out, cloneChecks[gate.DuplID])
	}
	if opts.Jscpd {
		out = append(out, cloneChecks[gate.JscpdID])
	}
	return out
}

// missingChecks returns the checks whose id the registry lacks.
func missingChecks(reg *registry.Registry, want []registry.Check) []registry.Check {
	var out []registry.Check
	for _, k := range want {
		if gate.ExternalCheck(reg, k.ID) == nil {
			out = append(out, k)
		}
	}
	return out
}

// ensureCloneChecks adds the wanted clone checks missing from the existing
// registry by a minimal text edit, or prints the YAML when no safe edit
// exists, and records a baseline for each check it added.
func ensureCloneChecks(root string, reg *registry.Registry, add []registry.Check, res *Result) (*registry.Registry, error) {
	if len(add) == 0 {
		return reg, nil
	}
	full := filepath.Join(root, filepath.FromSlash(registry.Path))
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	text, ok := appendChecks(string(data), add)
	if !ok {
		res.notice("could not add checks to %s without rewriting it; add under checks:\n%s", registry.Path, checkItems(add, "  "))
		return reg, nil
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return nil, err
	}
	if !slices.Contains(res.Created, registry.Path) {
		res.updated(registry.Path)
	}
	if reg, err = registry.Parse([]byte(text)); err != nil {
		return nil, err
	}
	baselineChecks(root, add, res)
	return reg, nil
}

// baselineChecks records the current findings of each new check as
// accepted, so adoption is incremental: only new duplicates fail.
func baselineChecks(root string, ks []registry.Check, res *Result) {
	for _, k := range ks {
		path, _ := gate.BaselinePath(k.ID)
		_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		b, err := gate.RecordBaseline(root, k)
		if err != nil {
			res.notice("baseline for checks.%s not recorded (%v); run 'arc baseline %s'", k.ID, err, k.ID)
			continue
		}
		if errors.Is(statErr, os.ErrNotExist) {
			res.created(path)
		} else {
			res.updated(path)
		}
		res.notice("checks.%s: %d existing finding(s) accepted in %s; only new ones fail arc gate", k.ID, b.Findings, path)
	}
}

// checkItems renders checks as block sequence items indented by ind.
func checkItems(ks []registry.Check, ind string) string {
	var b strings.Builder
	for _, k := range ks {
		q := make([]string, len(k.Command))
		for i, c := range k.Command {
			q[i] = strconv.Quote(c)
		}
		b.WriteString(ind + "- id: " + k.ID + "\n")
		b.WriteString(ind + "  command: [" + strings.Join(q, ", ") + "]\n")
		b.WriteString(ind + "  baseline: " + strconv.FormatBool(k.Baseline) + "\n")
	}
	return b.String()
}

// appendChecks returns text with ks appended to its top-level checks list,
// touching only the lines it must so comments and layout survive. It
// handles a missing checks key, "checks: []" on its own line and a block
// sequence; anything else, or an edit whose result does not parse to the
// same registry plus ks, reports false.
func appendChecks(text string, ks []registry.Check) (string, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return "", false
	}
	top := doc.Content[0]
	lines := strings.SplitAfter(text, "\n")
	var out string
	keyAt := -1
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "checks" {
			keyAt = i
		}
	}
	switch {
	case keyAt < 0:
		out = strings.TrimRight(text, "\n") + "\n\n# External tools run by 'arc gate'.\nchecks:\n" + checkItems(ks, "  ")
	case top.Content[keyAt+1].Kind != yaml.SequenceNode:
		return "", false
	case len(top.Content[keyAt+1].Content) == 0:
		key := top.Content[keyAt]
		line := lines[key.Line-1]
		body, comment, _ := strings.Cut(strings.TrimRight(line, "\n"), "#")
		if key.Column != 1 || strings.TrimSpace(body) != "checks: []" {
			return "", false
		}
		head := "checks:"
		if comment != "" {
			head += " #" + comment
		}
		lines[key.Line-1] = head + "\n" + checkItems(ks, "  ")
		out = strings.Join(lines, "")
	default:
		seq := top.Content[keyAt+1]
		if seq.Style&yaml.FlowStyle != 0 {
			return "", false
		}
		first := lines[seq.Content[0].Line-1]
		dash := strings.Index(first, "-")
		if dash < 0 || strings.TrimSpace(first[:dash]) != "" {
			return "", false
		}
		end := len(lines) // the next top-level key, or EOF
		if keyAt+2 < len(top.Content) {
			end = top.Content[keyAt+2].Line - 1
		}
		at := end // insert after the sequence's last content line
		for at > seq.Content[0].Line {
			l := strings.TrimSpace(lines[at-1])
			if l != "" && !strings.HasPrefix(lines[at-1], "#") {
				break
			}
			at--
		}
		if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
			lines[at-1] += "\n"
		}
		lines = append(lines[:at], append([]string{checkItems(ks, first[:dash])}, lines[at:]...)...)
		out = strings.Join(lines, "")
	}
	return out, sameButChecks(text, out, ks)
}

// sameButChecks verifies an edit: after parses to before plus ks.
func sameButChecks(before, after string, ks []registry.Check) bool {
	a, err1 := registry.Parse([]byte(before))
	b, err2 := registry.Parse([]byte(after))
	if err1 != nil || err2 != nil {
		return false
	}
	a.Checks = append(a.Checks, ks...)
	return reflect.DeepEqual(a, b)
}
