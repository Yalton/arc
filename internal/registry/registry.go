// Package registry loads, validates and queries .arch/registry.yaml, the
// source of truth for a repository's architecture (SPEC.md, "Registry schema").
package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Path is the registry location relative to the repository root.
const Path = ".arch/registry.yaml"

// DateLayout is the format of flags[].expires and removals[].remove_by.
const DateLayout = "2006-01-02"

type Registry struct {
	Version    int        `yaml:"version"`
	System     System     `yaml:"system"`
	NonGoals   []NonGoal  `yaml:"non_goals"`
	Ignore     []string   `yaml:"ignore"`
	Concepts   []Concept  `yaml:"concepts"`
	Boundaries []Boundary `yaml:"boundaries"`
	Flags      []Flag     `yaml:"flags"`
	Removals   []Removal  `yaml:"removals"`
	Modules    []Module   `yaml:"modules"`
	Limits     Limits     `yaml:"limits"`
	Checks     []Check    `yaml:"checks"`
	// Beads marks a repository that tracks work in beads; the agent snippet
	// then carries the bead template line. It is a registry field rather
	// than a probe for .beads/ so rendering depends only on tracked files.
	Beads bool `yaml:"beads"`
}

type System struct {
	Name    string `yaml:"name"`
	Summary string `yaml:"summary"`
}

type NonGoal struct {
	ID       string   `yaml:"id"`
	Text     string   `yaml:"text"`
	Keywords []string `yaml:"keywords"`
}

type Concept struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Summary string   `yaml:"summary"`
	Paths   []string `yaml:"paths"`
	Storage []string `yaml:"storage"`
	Env     []string `yaml:"env"`
	Aliases []string `yaml:"aliases"`
	Rules   []string `yaml:"rules"`
	Reason  string   `yaml:"reason"` // why the concept exists
	Draft   bool     `yaml:"draft"`
}

type Boundary struct {
	ID     string   `yaml:"id"`
	From   []string `yaml:"from"`
	Deny   []string `yaml:"deny"`
	Reason string   `yaml:"reason"`
}

type Flag struct {
	Name    string `yaml:"name"`
	Concept string `yaml:"concept"`
	Purpose string `yaml:"purpose"`
	Expires string `yaml:"expires"`
	Bead    string `yaml:"bead"`
}

type Removal struct {
	ID         string   `yaml:"id"`
	What       string   `yaml:"what"`
	Paths      []string `yaml:"paths"`
	ReplacedBy string   `yaml:"replaced_by"`
	RemoveBy   string   `yaml:"remove_by"`
	Bead       string   `yaml:"bead"`
}

// Module is installed third-party code the project uses but never edits
// (SPEC.md, "Modules"). Paths may be empty (a Go module lives outside the repo).
type Module struct {
	ID      string   `yaml:"id" json:"id"`
	Summary string   `yaml:"summary" json:"summary,omitempty"`
	Paths   []string `yaml:"paths" json:"paths,omitempty"`
	Covers  []string `yaml:"covers" json:"covers,omitempty"`
	Entry   []string `yaml:"entry" json:"entry,omitempty"`
	Import  string   `yaml:"import" json:"import,omitempty"`
	Rules   []string `yaml:"rules" json:"rules,omitempty"`
	Source  string   `yaml:"source" json:"source,omitempty"`
}

// Surface names the module's public surface for messages: its entry list
// joined with ", ", or "import <import>" when entry is empty, or "" with
// neither.
func (m Module) Surface() string {
	if len(m.Entry) > 0 {
		return strings.Join(m.Entry, ", ")
	}
	if m.Import != "" {
		return "import " + m.Import
	}
	return ""
}

// EditDenial is the sentence that refuses an edit of rel, a path under m
// (SPEC.md, "arc hook"): where the path is, the module's rules, and its
// public surface.
func (m Module) EditDenial(rel string) string {
	parts := []string{rel + " is inside installed module " + m.ID + "; do not edit it."}
	parts = append(parts, m.Rules...)
	if s := m.Surface(); s != "" {
		parts = append(parts, "Use its public surface: "+s+".")
	}
	return strings.Join(parts, " ")
}

type Limits struct {
	FileLines     int `yaml:"file_lines"`
	FunctionLines int `yaml:"function_lines"`
}

type Check struct {
	ID       string   `yaml:"id"`
	Command  []string `yaml:"command"`
	Baseline bool     `yaml:"baseline"`
}

// ErrNotFound is returned by Load when the repository has no registry.
var ErrNotFound = errors.New(Path + " not found (run 'arc init')")

// Load reads and parses the registry of the repository at root. Unknown
// keys are errors so typos do not silently disable a rule.
func Load(root string) (*Registry, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(Path)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes registry YAML.
func Parse(data []byte) (*Registry, error) {
	var r Registry
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("%s: %w%s", Path, err, misplacedHint(err))
	}
	return &r, nil
}

// unknownTop matches yaml.v3's error for an unknown key at the root.
var unknownTop = regexp.MustCompile(`field (\w+) not found in type registry\.Registry`)

// misplacedHint names where a root-level unknown key belongs when it is a
// field of a nested section (an agent once put a concept's env at the root).
func misplacedHint(err error) string {
	m := unknownTop.FindStringSubmatch(err.Error())
	if m == nil {
		return ""
	}
	var homes []string
	root := reflect.TypeOf(Registry{})
	for i := 0; i < root.NumField(); i++ {
		f := root.Field(i)
		t, list := f.Type, ""
		if t.Kind() == reflect.Slice {
			t, list = t.Elem(), "[]"
		}
		if t.Kind() != reflect.Struct {
			continue
		}
		for j := 0; j < t.NumField(); j++ {
			if strings.Split(t.Field(j).Tag.Get("yaml"), ",")[0] == m[1] {
				homes = append(homes, yamlName(f)+list+"."+m[1])
			}
		}
	}
	if len(homes) == 0 {
		return ""
	}
	return fmt.Sprintf("\n%s is not a top-level key; it goes under %s. Move it and edit the field there.", m[1], strings.Join(homes, " or "))
}

func yamlName(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("yaml"), ",")[0]
}

// Concept returns the concept with id, or nil.
func (r *Registry) Concept(id string) *Concept {
	for i := range r.Concepts {
		if r.Concepts[i].ID == id {
			return &r.Concepts[i]
		}
	}
	return nil
}

// ModuleOf returns the module whose paths match path, if any. Module paths
// take precedence over concept paths (SPEC.md, "Modules"). A glob such as
// "dir/**" also matches the gitlink path "dir" of a submodule.
func (r *Registry) ModuleOf(path string) (*Module, bool) {
	for i := range r.Modules {
		if matchAny(r.Modules[i].Paths, path) {
			return &r.Modules[i], true
		}
	}
	return nil, false
}

// Owner returns the concept whose paths match path (repo-relative,
// slash-separated), or nil. A path under a module has no concept. Validation
// guarantees at most one match.
func (r *Registry) Owner(path string) *Concept {
	if _, ok := r.ModuleOf(path); ok {
		return nil
	}
	for i := range r.Concepts {
		if matchAny(r.Concepts[i].Paths, path) {
			return &r.Concepts[i]
		}
	}
	return nil
}

// BuiltinIgnore lists files arc itself manages or installs (including the
// harness adapter directories init writes into); they are always ignored, so
// registries written before an adapter existed still pass placement.
var BuiltinIgnore = []string{
	".arch/**", ArchitectureFile, "AGENTS.md", "CLAUDE.md",
	".claude/**", ".codex/**", ".opencode/**", ".gitmodules",
}

// IsIgnored reports whether path is exempt from placement checks.
func (r *Registry) IsIgnored(path string) bool {
	return matchAny(BuiltinIgnore, path) || matchAny(r.Ignore, path)
}

// HasDrafts reports whether any concept is still a draft.
func (r *Registry) HasDrafts() bool {
	for _, c := range r.Concepts {
		if c.Draft {
			return true
		}
	}
	return false
}

// ExpiresAt parses the flag's expiry date.
func (f Flag) ExpiresAt() (time.Time, error) { return time.Parse(DateLayout, f.Expires) }

// RemoveByAt parses the removal's deadline.
func (rm Removal) RemoveByAt() (time.Time, error) { return time.Parse(DateLayout, rm.RemoveBy) }

// Match reports whether path matches the glob pattern.
func Match(pattern, path string) bool {
	ok, _ := doublestar.Match(pattern, path)
	return ok
}

func matchAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if Match(p, path) {
			return true
		}
	}
	return false
}

// SubmoduleDirty is the gate finding and hook feedback text, after
// "module <id>: ", for a module submodule with modified tracked files.
const SubmoduleDirty = "submodule has local changes"
