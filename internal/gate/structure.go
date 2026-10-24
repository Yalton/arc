package gate

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"arc/internal/registry"
)

// sqlIdent matches one SQL identifier: double-quoted, backquoted,
// bracketed or bare.
const sqlIdent = "(?:\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]|[A-Za-z_][A-Za-z0-9_$]*)"

var createTableRe = regexp.MustCompile(`(?i)\bcreate\s+table\s+(?:if\s+not\s+exists\s+)?(` + sqlIdent + `(?:\s*\.\s*` + sqlIdent + `)?)`)

// quoted matches a string literal in either quote style, capturing the body
// in one of two groups.
const quoted = `(?:"([^"]+)"|'([^']+)')`

var (
	goEnvRe = []*regexp.Regexp{
		regexp.MustCompile("\\bos\\.(?:Getenv|LookupEnv)\\(\\s*(?:\"([^\"]+)\"|`([^`]+)`)"),
	}
	pyEnvRe = []*regexp.Regexp{
		regexp.MustCompile(`\bos\.environ\[\s*` + quoted + `\s*\]`),
		regexp.MustCompile(`\bos\.environ\.get\(\s*` + quoted),
		regexp.MustCompile(`\bos\.getenv\(\s*` + quoted),
	}
	jsEnvRe = []*regexp.Regexp{
		regexp.MustCompile(`\bprocess\.env\.([A-Za-z_$][A-Za-z0-9_$]*)`),
		regexp.MustCompile("\\bprocess\\.env\\[\\s*(?:\"([^\"]+)\"|'([^']+)'|`([^`$]+)`)\\s*\\]"),
	}
)

// envPatterns returns the env-var read patterns for a file's language,
// chosen by extension; nil for other languages.
func envPatterns(p string) []*regexp.Regexp {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return goEnvRe
	case ".py":
		return pyEnvRe
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return jsEnvRe
	}
	return nil
}

// firstGroup returns the first non-empty capture group of m.
func firstGroup(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// unquoteIdent strips SQL identifier quoting from each dot-separated part
// and lowercases the result.
func unquoteIdent(s string) string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && (p[0] == '"' || p[0] == '`' || p[0] == '[') {
			p = p[1 : len(p)-1]
		}
		parts[i] = strings.ToLower(p)
	}
	return strings.Join(parts, ".")
}

// checkStructure fails added lines that create a table no concept lists in
// storage, or read an env var no concept lists in env and no flag names.
// Only added lines are scanned, so names that appear only in removed or
// unchanged lines never fire. Ignored files (tests, docs, fixtures) are
// skipped: tables and env vars they mention are not real structure.
func checkStructure(ctx *Context) ([]Finding, error) {
	tables := map[string]bool{}
	envs := map[string]bool{}
	for _, c := range ctx.Reg.Concepts {
		for _, s := range c.Storage {
			tables[unquoteIdent(s)] = true
		}
		for _, e := range c.Env {
			envs[e] = true
		}
	}
	for _, f := range ctx.Reg.Flags {
		envs[f.Name] = true
	}

	var out []Finding
	for _, fc := range ctx.Changes.Files {
		if ctx.Reg.IsIgnored(fc.Path) {
			continue
		}
		owner := ctx.Reg.Owner(fc.Path)
		envRes := envPatterns(fc.Path)
		seen := map[string]bool{}
		for _, ln := range fc.Added {
			for _, m := range createTableRe.FindAllStringSubmatch(ln.Text, -1) {
				name := unquoteIdent(m[1])
				short := name
				if i := strings.LastIndex(name, "."); i >= 0 {
					short = name[i+1:]
				}
				if tables[name] || tables[short] || seen["t:"+name] {
					continue
				}
				seen["t:"+name] = true
				out = append(out, tableFinding(owner, fc.Path, ln.Num, name))
			}
			for _, re := range envRes {
				for _, m := range re.FindAllStringSubmatch(ln.Text, -1) {
					name := firstGroup(m)
					if name == "" || envs[name] || seen["e:"+name] {
						continue
					}
					seen["e:"+name] = true
					out = append(out, envFinding(owner, fc.Path, ln.Num, name))
				}
			}
		}
	}
	return out, nil
}

func tableFinding(owner *registry.Concept, p string, line int, name string) Finding {
	f := Finding{Path: p, Line: line, Entry: "concepts"}
	if owner != nil {
		f.Entry = fmt.Sprintf("concepts[%s].storage", owner.ID)
		f.Message = fmt.Sprintf("new table %q is not in any concept's storage. Add it to the owning concept's storage in %s (owner of this file: %s), or reuse an existing table.", name, registry.Path, owner.ID)
	} else {
		f.Message = fmt.Sprintf("new table %q is not in any concept's storage. Add it to the owning concept's storage in %s (this file has no owner; add it to a concept's paths first), or reuse an existing table.", name, registry.Path)
	}
	return f
}

func envFinding(owner *registry.Concept, p string, line int, name string) Finding {
	f := Finding{Path: p, Line: line, Entry: "concepts"}
	if owner != nil {
		f.Entry = fmt.Sprintf("concepts[%s].env", owner.ID)
		f.Message = fmt.Sprintf("env var %q is not registered. Add it to the owning concept's env (owner: %s) or to flags in %s.", name, owner.ID, registry.Path)
	} else {
		f.Message = fmt.Sprintf("env var %q is not registered. Add it to the owning concept's env (this file has no owner; add it to a concept's paths first) or to flags in %s.", name, registry.Path)
	}
	return f
}
