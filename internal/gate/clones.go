package gate

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Yalton/arc/internal/registry"
)

// Clone detectors are external checks with a known id (set up by arc init,
// SPEC.md "arc init"). Their output is rewritten to one canonical line per
// clone, "<path>:<start>-<end>: duplicate of <path>:<start>-<end>", so the
// baseline survives line shifts and the gate can say what to do instead.
const (
	DuplID  = "dupl"
	JscpdID = "jscpd"
)

// IsCloneDetector reports whether check id is one of the clone detectors.
func IsCloneDetector(id string) bool { return id == DuplID || id == JscpdID }

// CloneMessage is the gate message for one clone detector finding.
func CloneMessage(line string) string {
	return "duplicates existing code: " + line + "; extend or reuse the existing code instead of copying"
}

var (
	// jscpd --reporters xcode, verified with jscpd 5.4.2:
	// "a.js:1:14: warning: Found 8 lines (1-9) duplicated on file b.js (1-9)".
	jscpdClone = regexp.MustCompile(`^(\S+):\d+:\d+: warning: Found \d+ lines \((\d+)-(\d+)\) duplicated on file (\S+) \((\d+)-(\d+)\)$`)
	// jscpd's trailing summary; the time changes every run.
	jscpdNoise = regexp.MustCompile(`^(Found \d+ clones\.|time: .*)$`)
	// The canonical clone line.
	cloneRE = regexp.MustCompile(`^(\S+):\d+-\d+: duplicate of (\S+):\d+-\d+$`)
)

// cloneLines canonicalizes a clone detector's output lines and sorts them,
// since jscpd may report in worker order. dupl -plumbing already prints the
// canonical form. A clone with either side in a test file (registry.IsTest)
// or a file reg ignores is dropped: table-driven tests repeat by design, and
// ignored files are not judged. Lines in no known shape (errors, a changed
// format) are kept as they are, so a misconfigured tool is visible rather
// than silent.
func cloneLines(id string, lines []string, reg *registry.Registry) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if id == JscpdID {
			if jscpdNoise.MatchString(l) {
				continue
			}
			if m := jscpdClone.FindStringSubmatch(l); m != nil {
				l = m[1] + ":" + m[2] + "-" + m[3] + ": duplicate of " + m[4] + ":" + m[5] + "-" + m[6]
			}
		}
		if m := cloneRE.FindStringSubmatch(l); m != nil && (exempt(reg, m[1]) || exempt(reg, m[2])) {
			continue
		}
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// exempt reports whether clones touching path p are not judged.
func exempt(reg *registry.Registry, p string) bool {
	p = strings.TrimPrefix(p, "./")
	return registry.IsTest(p) || (reg != nil && reg.IsIgnored(p))
}
