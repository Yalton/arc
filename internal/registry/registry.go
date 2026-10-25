// Package registry loads, validates and queries .arch/registry.yaml, the
// source of truth for a repository's architecture (SPEC.md, "Registry schema").
package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		return nil, fmt.Errorf("%s: %w", Path, err)
	}
	return &r, nil
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

// Owner returns the concept whose paths match path (repo-relative,
// slash-separated), or nil. Validation guarantees at most one match.
func (r *Registry) Owner(path string) *Concept {
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
	".claude/**", ".codex/**", ".opencode/**",
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
