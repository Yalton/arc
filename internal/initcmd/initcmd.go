// Package initcmd implements 'arc init': the registry skeleton (optionally
// with --scan draft concepts), .arch/.gitignore, the rendered blocks and the
// pre-commit hook (SPEC.md, "arc init"). Every step is idempotent.
package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"arc/internal/gitx"
	"arc/internal/registry"
)

// Options selects optional init behaviour.
type Options struct {
	Scan   bool // propose draft concepts from the directory layout
	Claude bool // add arc's hooks to .claude/settings.json
}

// Result lists what Run did. Paths are repo-relative and slash-separated
// (a hooks directory outside the repository is reported absolute).
type Result struct {
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	Notices []string `json:"notices"`
}

func (r *Result) created(p string) { r.Created = append(r.Created, p) }
func (r *Result) updated(p string) { r.Updated = append(r.Updated, p) }
func (r *Result) notice(f string, args ...any) {
	r.Notices = append(r.Notices, fmt.Sprintf(f, args...))
}

// gitignorePath is the .arch/.gitignore location relative to root.
const gitignorePath = ".arch/.gitignore"

// Run initializes arc in the repository at root.
func Run(root string, opts Options) (*Result, error) {
	res := &Result{Created: []string{}, Updated: []string{}, Notices: []string{}}
	reg, err := ensureRegistry(root, opts, res)
	if err != nil {
		return res, err
	}
	if err := ensureGitignore(root, res); err != nil {
		return res, err
	}
	if err := render(root, reg, res); err != nil {
		return res, err
	}
	if err := ensureHook(root, res); err != nil {
		return res, err
	}
	if opts.Claude {
		if err := ensureClaude(root, res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// ensureRegistry writes the skeleton if no registry exists and returns the
// parsed registry either way. An existing registry is never rewritten.
func ensureRegistry(root string, opts Options, res *Result) (*registry.Registry, error) {
	full := filepath.Join(root, filepath.FromSlash(registry.Path))
	if _, err := os.Stat(full); err == nil {
		res.notice("%s exists; left unchanged", registry.Path)
		if opts.Scan {
			res.notice("--scan ignored: %s exists", registry.Path)
		}
		return registry.Load(root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var concepts []draft
	if opts.Scan {
		files, err := registry.RepoFiles(root)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		var rootFiles int
		concepts, rootFiles = scan(files, defaultIgnore)
		res.notice("--scan proposed %d draft concept(s); refine them, then remove draft: true", len(concepts))
		if rootFiles > 0 {
			res.notice("--scan left %d root-level file(s) unowned", rootFiles)
		}
	}
	text := skeleton(systemName(root), concepts)
	reg, err := registry.Parse([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("internal error: generated registry does not parse: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return nil, err
	}
	res.created(registry.Path)
	return reg, nil
}

func systemName(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Base(root)
}

// ensureGitignore makes .arch/.gitignore contain a cache/ line.
func ensureGitignore(root string, res *Result) error {
	full := filepath.Join(root, filepath.FromSlash(gitignorePath))
	data, err := os.ReadFile(full)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.WriteFile(full, []byte("cache/\n"), 0o644); err != nil {
			return err
		}
		res.created(gitignorePath)
		return nil
	case err != nil:
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if l := strings.TrimSpace(line); l == "cache/" || l == "cache" || l == "/cache/" {
			return nil
		}
	}
	s := string(data)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if err := os.WriteFile(full, []byte(s+"cache/\n"), 0o644); err != nil {
		return err
	}
	res.updated(gitignorePath)
	return nil
}

// render writes ARCHITECTURE.md and the agent snippets, classifying each
// changed file as created or updated.
func render(root string, reg *registry.Registry, res *Result) error {
	existed := map[string]bool{}
	for _, f := range append([]string{registry.ArchitectureFile}, registry.SnippetFiles...) {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			existed[f] = true
		}
	}
	changed, err := reg.Render(root)
	for _, f := range changed {
		if existed[f] {
			res.updated(f)
		} else {
			res.created(f)
		}
	}
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

// relOrAbs reports p relative to root when it lies inside it.
func relOrAbs(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return p
}

// gitTop returns the git top-level for root, or "" outside a repository.
func gitTop(root string) string {
	top, err := gitx.Root(root)
	if err != nil {
		return ""
	}
	return top
}
