package initcmd

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"arc/internal/registry"
)

// conventionalRoots hold packages one level down; --scan proposes a concept
// per second-level directory beneath them.
var conventionalRoots = map[string]bool{
	"internal": true, "pkg": true, "src": true, "cmd": true,
	"lib": true, "apps": true, "packages": true,
}

// draft is one concept proposed by --scan.
type draft struct {
	id, name string
	path     string // single glob owning the directory
	parent   string // conventional root, "" for top-level directories
	base     string // directory name the id derives from
}

// scan proposes draft concepts for files (repo-relative) that are not
// ignored. It returns them sorted by path, plus the number of non-ignored
// root-level files left unowned.
func scan(files, ignore []string) ([]draft, int) {
	ignored := func(f string) bool {
		for _, p := range ignore {
			if registry.Match(p, f) {
				return true
			}
		}
		return false
	}
	byPath := map[string]draft{}
	rootFiles := 0
	for _, f := range files {
		if ignored(f) {
			continue
		}
		parts := strings.Split(f, "/")
		if len(parts) == 1 {
			rootFiles++
			continue
		}
		top := parts[0]
		if strings.HasPrefix(top, ".") {
			continue
		}
		var d draft
		switch {
		case !conventionalRoots[top]:
			d = draft{path: globEscape(top) + "/**", base: top}
		case len(parts) == 2:
			// Files directly in the conventional root: '*' does not cross
			// '/', so this cannot overlap the second-level concepts.
			d = draft{path: globEscape(top) + "/*", base: top}
		case strings.HasPrefix(parts[1], "."):
			continue
		default:
			d = draft{path: globEscape(top) + "/" + globEscape(parts[1]) + "/**", base: parts[1], parent: top}
		}
		byPath[d.path] = d
	}
	out := make([]draft, 0, len(byPath))
	for _, d := range byPath {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	assignIDs(out)
	return out, rootFiles
}

// assignIDs derives ids from directory names; names shared by several
// drafts are prefixed with their parent (cmd-arc), and numbered if still
// ambiguous.
func assignIDs(ds []draft) {
	count := map[string]int{}
	for _, d := range ds {
		count[slug(d.base)]++
	}
	used := map[string]bool{}
	for i := range ds {
		id := slug(ds[i].base)
		if count[id] > 1 && ds[i].parent != "" {
			id = slug(ds[i].parent) + "-" + id
		}
		if used[id] {
			n := 2
			for used[id+"-"+strconv.Itoa(n)] {
				n++
			}
			id += "-" + strconv.Itoa(n)
		}
		used[id] = true
		ds[i].id = id
		ds[i].name = title(id)
	}
}

// slug lowercases s and replaces runs of non-alphanumerics with '-'.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	id := strings.TrimRight(b.String(), "-")
	if id == "" {
		id = "concept"
	}
	return id
}

// title turns an id like cmd-arc into "Cmd Arc".
func title(id string) string {
	words := strings.Split(id, "-")
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// globEscape backslash-escapes doublestar metacharacters in a literal path
// component, so directories like "[locale]" match themselves.
func globEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`*?[]{}\\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
