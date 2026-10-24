package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxNewShown is how many new external-check findings the text lists per
// check; --json has all of them.
const MaxNewShown = 10

// Text renders the report as plain text, for people and for the
// architect prompt.
func (r *Report) Text() string {
	var w strings.Builder
	base := "no commit before the window; history starts inside it"
	if r.Base != "" {
		base = "base " + short(r.Base)
	}
	fmt.Fprintf(&w, "arc audit: %s to %s (since %s; %s)\n", r.From, r.To, r.Since, base)

	w.WriteString("\nNon-test LOC per concept (window start -> now):\n")
	if len(r.Concepts) == 0 {
		w.WriteString("  none\n")
	}
	width := 0
	for _, c := range r.Concepts {
		width = max(width, len(c.ID))
	}
	for _, c := range r.Concepts {
		fmt.Fprintf(&w, "  %-*s %6d -> %6d  (%+d)\n", width, c.ID, c.Before, c.Now, c.Delta)
	}

	h := r.History
	ratio := "n/a"
	if h.FixFeatRatio != nil {
		ratio = fmt.Sprintf("%.2f", *h.FixFeatRatio)
	}
	fmt.Fprintf(&w, "\nHistory: %d commit(s), +%d -%d lines; fix: %d, feat: %d (fix/feat %s)\n",
		h.Commits, h.Added, h.Deleted, h.Fix, h.Feat, ratio)

	w.WriteString("\nHotspots (files by fix commits):\n")
	if len(r.Hotspots) == 0 {
		w.WriteString("  none\n")
	}
	for _, s := range r.Hotspots {
		fmt.Fprintf(&w, "  %3d  %s\n", s.FixCommits, s.Path)
	}

	list(&w, "Unowned files", r.Unowned)
	list(&w, "Draft concepts", r.Drafts)
	list(&w, "Concepts without a reason", r.NoReason)
	var multi []string
	for _, c := range r.MultiStorage {
		multi = append(multi, c.ID+": "+strings.Join(c.Storage, ", "))
	}
	list(&w, "Concepts owning several tables", multi)
	var flags []string
	for _, f := range r.ExpiredFlags {
		flags = append(flags, fmt.Sprintf("%s (%s) expired %s", f.Name, orNone(f.Concept), f.Expires)+bead(f.Bead))
	}
	list(&w, "Expired flags", flags)
	list(&w, "Overdue removals", removalLines(r.OverdueRemovals))
	list(&w, "Removals whose bead is closed but paths remain", removalLines(r.ClosedBeadRemovals))

	w.WriteString("\nExternal checks:\n")
	if len(r.Checks) == 0 {
		w.WriteString("  none configured\n")
	}
	for _, c := range r.Checks {
		if c.Error != "" {
			fmt.Fprintf(&w, "  %s: error: %s\n", c.ID, c.Error)
			continue
		}
		fmt.Fprintf(&w, "  %s: %d finding(s), %d baselined, %d new\n", c.ID, c.Findings, c.Baselined, len(c.New))
		for i, l := range c.New {
			if i == MaxNewShown {
				fmt.Fprintf(&w, "    ... and %d more\n", len(c.New)-MaxNewShown)
				break
			}
			w.WriteString("    " + l + "\n")
		}
	}
	for _, n := range r.Notices {
		w.WriteString("\nNotice: " + n + "\n")
	}
	return strings.TrimSuffix(w.String(), "\n")
}

func list(w *strings.Builder, title string, items []string) {
	fmt.Fprintf(w, "\n%s: ", title)
	if len(items) == 0 {
		w.WriteString("none\n")
		return
	}
	fmt.Fprintf(w, "%d\n", len(items))
	for _, it := range items {
		w.WriteString("  " + it + "\n")
	}
}

func removalLines(rs []RemovalItem) []string {
	var out []string
	for _, rm := range rs {
		s := rm.ID
		if rm.What != "" {
			s += " (" + rm.What + ")"
		}
		if rm.RemoveBy != "" {
			s += " due " + rm.RemoveBy
		}
		if rm.ReplacedBy != "" {
			s += ", replaced by " + rm.ReplacedBy
		}
		out = append(out, s+bead(rm.Bead))
	}
	return out
}

func bead(id string) string {
	if id == "" {
		return ""
	}
	return " [bead " + id + "]"
}

func orNone(s string) string {
	if s == "" {
		return "no concept"
	}
	return s
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// readRegular reads a repository file, and false for anything that is not
// a regular file.
func readRegular(root, rel string) ([]byte, bool, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false, err
	}
	data, err := os.ReadFile(p)
	return data, err == nil, err
}
