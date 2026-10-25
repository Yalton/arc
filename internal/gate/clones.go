package gate

import (
	"regexp"
	"sort"
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
)

// cloneLines canonicalizes a clone detector's output lines and sorts them,
// since jscpd may report in worker order. dupl -plumbing already prints the
// canonical form. Lines in no known shape (errors, a changed format) are
// kept as they are, so a misconfigured tool is visible rather than silent.
func cloneLines(id string, lines []string) []string {
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
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
