package initcmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Yalton/arc/internal/registry"
)

// Finding records how one init item was decided: by an explicit flag or by
// detection, which reads only the repository, the PATH list and git config.
type Finding struct {
	Item   string `json:"item"`   // claude, codex, opencode, ci, scan or beads
	Value  string `json:"value"`  // on/off, or the CI provider or none
	Source string `json:"source"` // "flag" or "detected"
	Reason string `json:"reason"` // why, e.g. ".claude/ exists"
}

// Detectable lists the flag names Resolve can fill in, in report order.
var Detectable = []string{"claude", "codex", "opencode", "ci", "scan", "beads"}

// Resolve fills every item whose flag is not in explicit from detection and
// returns the options to run with plus one Finding per item. pathList is a
// PATH-style list searched for the harness CLIs; explicit holds the flag
// names the user set. Explicit values win and are never re-detected.
func Resolve(root, pathList string, opts Options, explicit map[string]bool) (Options, []Finding) {
	var fs []Finding
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	for _, item := range Detectable {
		if explicit[item] {
			var v string
			switch item {
			case "claude":
				v = onOff(opts.Claude)
			case "codex":
				v = onOff(opts.Codex)
			case "opencode":
				v = onOff(opts.Opencode)
			case "scan":
				v = onOff(opts.Scan)
			case "beads":
				v = onOff(opts.Beads)
			case "ci":
				v = opts.CI
				if v == "" {
					v = "none"
				}
			}
			why := "--" + item
			switch {
			case item == "ci":
				why += " " + v
			case v == "off":
				why += "=false"
			}
			fs = append(fs, Finding{item, v, "flag", why})
			continue
		}
		var on bool
		var why string
		switch item {
		case "claude":
			on, why = detectHarness(root, pathList, ".claude", "claude")
			opts.Claude = on
		case "codex":
			on, why = detectHarness(root, pathList, ".codex", "codex")
			opts.Codex = on
		case "opencode":
			on, why = detectHarness(root, pathList, ".opencode", "opencode")
			opts.Opencode = on
		case "scan":
			on, why = detectScan(root)
			opts.Scan = on
		case "beads":
			on, why = detectBeads(root)
			opts.Beads = on
		case "ci":
			provider, why := detectCI(root)
			opts.CI, opts.CIDetected = provider, provider != ""
			if provider == "" {
				provider = "none"
			}
			fs = append(fs, Finding{item, provider, "detected", why})
			continue
		}
		fs = append(fs, Finding{item, onOff(on), "detected", why})
	}
	return opts, fs
}

// detectHarness reports a harness present when its project directory exists
// or its CLI is on pathList.
func detectHarness(root, pathList, dir, bin string) (bool, string) {
	if fi, err := os.Stat(filepath.Join(root, dir)); err == nil && fi.IsDir() {
		return true, dir + "/ exists"
	}
	if p := lookPath(pathList, bin); p != "" {
		return true, bin + " on PATH (" + p + ")"
	}
	return false, "no " + dir + "/ and " + bin + " not on PATH"
}

// lookPath returns the first executable name in the directories of
// pathList, or "". Empty entries (the current directory) are skipped.
func lookPath(pathList, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if err == nil && fi.Mode().IsRegular() && (runtime.GOOS == "windows" || fi.Mode()&0o111 != 0) {
			return p
		}
	}
	return ""
}

// detectScan turns --scan on only when init will create the registry and
// the repository has a file the skeleton would not ignore.
func detectScan(root string) (bool, string) {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(registry.Path))); !errors.Is(err, os.ErrNotExist) {
		return false, registry.Path + " exists"
	}
	files, err := registry.RepoFiles(root)
	if err != nil {
		return false, "cannot list files: " + err.Error()
	}
	n := 0
	for _, f := range files {
		if !matchAny(registry.BuiltinIgnore, f) && !matchAny(defaultIgnore, f) {
			n++
		}
	}
	if n == 0 {
		return false, "new registry but no files to scan"
	}
	return true, fmt.Sprintf("new registry and %d file(s) to scan", n)
}

// beadsDir is the beads workspace directory. Init reads it only to choose
// what to write; render never does, since a fresh clone lacks it.
const beadsDir = ".beads"

func hasBeadsDir(root string) bool {
	fi, err := os.Stat(filepath.Join(root, beadsDir))
	return err == nil && fi.IsDir()
}

// detectBeads turns beads: true on for a new registry when the repository
// has a beads workspace. An existing registry is never rewritten; Run then
// prints a notice instead.
func detectBeads(root string) (bool, string) {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(registry.Path))); !errors.Is(err, os.ErrNotExist) {
		return false, registry.Path + " exists"
	}
	if hasBeadsDir(root) {
		return true, beadsDir + "/ exists"
	}
	return false, "no " + beadsDir + "/ directory"
}

func matchAny(patterns []string, f string) bool {
	for _, p := range patterns {
		if registry.Match(p, f) {
			return true
		}
	}
	return false
}

// detectCI maps the origin remote's host to a provider: github.com is
// github, GitLab and Bitbucket have no arc workflow, and any other host is
// taken for Gitea or Forgejo. Only the host is reported, never the URL,
// which may carry credentials.
func detectCI(root string) (string, string) {
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "no origin remote"
	}
	host := strings.ToLower(remoteHost(strings.TrimSpace(string(out))))
	switch {
	case host == "":
		return "", "origin is a local path"
	case host == "github.com" || host == "www.github.com":
		return "github", "origin host is github.com"
	case host == "gitlab.com" || strings.HasPrefix(host, "gitlab.") || host == "bitbucket.org":
		return "", "origin host " + host + " has no arc workflow (github or gitea only)"
	}
	return "gitea", "origin host " + host + " is not github.com; assuming Gitea or Forgejo (use --ci github for GitHub Enterprise)"
}

// remoteHost extracts the host from a git remote URL: scheme://[user@]host/…
// or scp-like [user@]host:path. Local paths yield "".
func remoteHost(u string) string {
	if strings.Contains(u, "://") {
		p, err := url.Parse(u)
		if err != nil {
			return ""
		}
		return p.Hostname()
	}
	i := strings.Index(u, ":")
	if i <= 0 || strings.Contains(u[:i], "/") {
		return ""
	}
	h := u[:i]
	if j := strings.LastIndex(h, "@"); j >= 0 {
		h = h[j+1:]
	}
	if len(h) == 1 { // a Windows drive letter, C:\…
		return ""
	}
	return h
}

// foreignCI reports the workflow path for provider when it exists without
// arc's marker, so a detected (not requested) provider can be skipped.
func foreignCI(root, provider string) (string, bool) {
	rel := ciPaths[provider]
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return rel, false
	}
	return rel, !strings.HasPrefix(string(data), ciMarker)
}
