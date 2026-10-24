package gate

import (
	"testing"
)

const boundariesRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["*.md", ".arch/**", "go.mod"]
concepts:
  - id: app
    name: App
    summary: App.
    paths: ["internal/**", "py/**", "web/**"]
boundaries:
  - id: writes-through-actions
    from: ["internal/api/**", "py/api/**", "web/api/**"]
    deny: ["internal/store/**", "py/store/**", "web/store/**"]
    reason: All writes go through actions.
`

func boundariesRepo(t *testing.T) *testRepo {
	r := newRepo(t, boundariesRegistry)
	r.write("go.mod", "module example.com/demo\n\ngo 1.22\n")
	r.write("internal/store/store.go", "package store\n\nfunc Save() {}\n")
	r.write("internal/actions/actions.go", "package actions\n\nfunc Do() {}\n")
	r.write("py/__init__.py", "")
	r.write("py/store/__init__.py", "")
	r.write("py/store/db.py", "def save(): pass\n")
	r.write("py/actions.py", "def do(): pass\n")
	r.write("web/store/db.ts", "export const save = () => {};\n")
	r.write("web/actions.ts", "export const act = () => {};\n")
	r.commitAll("init")
	return r
}

const crossingGo = `package api

import (
	"example.com/demo/internal/actions"
	"example.com/demo/internal/store"
)

func Handle() { actions.Do(); store.Save() }
`

func TestBoundariesGoCrossingFails(t *testing.T) {
	r := boundariesRepo(t)
	r.write("internal/api/api.go", crossingGo)
	r.git("add", "-A")
	rep := r.gate("staged", "boundaries")
	wantFindings(t, rep.Findings,
		"internal/api/api.go:5: imports example.com/demo/internal/store (internal/store), crossing boundary writes-through-actions: All writes go through actions. Route it through the allowed layer or change the registry with a reason.")
	if rep.Findings[0].Entry != "boundaries[writes-through-actions]" {
		t.Fatalf("entry %q", rep.Findings[0].Entry)
	}
}

func TestBoundariesAllowedImportsPass(t *testing.T) {
	r := boundariesRepo(t)
	r.write("internal/api/api.go", "package api\n\nimport \"example.com/demo/internal/actions\"\n\nfunc Handle() { actions.Do() }\n")
	r.write("internal/other/other.go", "package other\n\nimport \"example.com/demo/internal/store\"\n\nvar _ = store.Save\n")
	r.write("py/api/h.py", "from py import actions\n")
	r.write("web/api/h.ts", "import { act } from \"../actions\";\nimport React from \"react\";\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "boundaries"); len(rep.Findings) != 0 {
		t.Fatalf("unexpected:\n%s", findings(rep))
	}
}

func TestBoundariesPythonAndTSCrossingsFail(t *testing.T) {
	r := boundariesRepo(t)
	r.write("py/api/__init__.py", "")
	r.write("py/api/h.py", "import os\nfrom ..store import db\nimport py.store.db\n")
	r.write("web/api/h.ts", "import {\n  save,\n} from \"../store/db\";\nconst s = require(\"../store/db.js\");\n")
	r.git("add", "-A")
	rep := r.gate("staged", "boundaries")
	wantFindings(t, rep.Findings,
		"py/api/h.py:2: imports ..store.db (py/store/db.py), crossing boundary writes-through-actions",
		"py/api/h.py:3: imports py.store.db (py/store/db.py)",
		"web/api/h.ts:3: imports ../store/db (web/store/db.ts)",
		"web/api/h.ts:4: imports ../store/db.js (web/store/db.ts)")
}

func TestBoundariesPreExistingCrossingPassesUnlessAll(t *testing.T) {
	r := boundariesRepo(t)
	r.write("internal/api/api.go", crossingGo)
	r.commitAll("pre-existing crossing")
	r.git("checkout", "-q", "-b", "feature")
	r.write("internal/api/api.go", crossingGo+"\nfunc More() {}\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "boundaries"); len(rep.Findings) != 0 {
		t.Fatalf("staged: untouched crossing should pass:\n%s", findings(rep))
	}
	r.commitAll("touch")
	if rep := r.gate("main", "boundaries"); len(rep.Findings) != 0 {
		t.Fatalf("base: untouched crossing should pass:\n%s", findings(rep))
	}
	wantFindings(t, r.gate("all", "boundaries").Findings, "internal/api/api.go:5: imports example.com/demo/internal/store")
}

func TestBoundariesNewCrossingInTouchedFile(t *testing.T) {
	r := boundariesRepo(t)
	r.write("py/api/h.py", "from py.store import db\n")
	r.commitAll("pre-existing crossing")
	r.write("py/api/h.py", "from py.store import db\nimport py.store\n")
	r.git("add", "-A")
	wantFindings(t, r.gate("staged", "boundaries").Findings, "py/api/h.py:2: imports py.store (py/store)")
}

func TestBoundariesStagedContentIsJudged(t *testing.T) {
	r := boundariesRepo(t)
	r.write("web/api/h.ts", "import { save } from \"../store/db\";\n")
	r.git("add", "-A")
	r.write("web/api/h.ts", "import { act } from \"../actions\";\n") // unstaged fix
	wantFindings(t, r.gate("staged", "boundaries").Findings, "web/api/h.ts:1: imports ../store/db")
	if rep := r.gate("working", "boundaries"); len(rep.Findings) != 0 {
		t.Fatalf("working tree has no crossing:\n%s", findings(rep))
	}
}
