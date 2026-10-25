package gate

import (
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/gitx"
	"github.com/Yalton/arc/internal/registry"
)

const structureRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["**/*_test.go", "**/*_test.py", "*.md", ".arch/**"]
concepts:
  - id: goal
    name: Goal
    summary: Goals.
    paths: ["internal/goal/**", "web/**", "py/**", "db/**"]
    storage: [goals, "audit.events"]
    env: [GOAL_URL]
flags:
  - name: NEW_UI
    concept: goal
    purpose: Toggle the new UI.
    expires: "2027-01-01"
    bead: arc-1
`

func structureRepo(t *testing.T) *testRepo {
	r := newRepo(t, structureRegistry)
	r.write("internal/goal/goal.go", "package goal\n")
	r.commitAll("init")
	return r
}

// structure stages everything and runs checkStructure on the staged diff.
func (r *testRepo) structure() []Finding {
	r.t.Helper()
	r.git("add", "-A")
	reg, err := registry.Load(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	cs, err := gitx.StagedChangeSet(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	fs, err := checkStructure(&Context{Root: r.root, Reg: reg, Changes: cs})
	if err != nil {
		r.t.Fatal(err)
	}
	return fs
}

func names(fs []Finding) string {
	var s []string
	for _, f := range fs {
		s = append(s, f.String())
	}
	return strings.Join(s, "\n")
}

func wantFindings(t *testing.T, fs []Finding, want ...string) {
	t.Helper()
	if len(fs) != len(want) {
		t.Fatalf("want %d findings, got %d:\n%s", len(want), len(fs), names(fs))
	}
	for i, w := range want {
		if !strings.Contains(fs[i].String(), w) {
			t.Fatalf("finding %d: want %q in\n%s", i, w, names(fs))
		}
	}
}

func TestStructureGoEnv(t *testing.T) {
	r := structureRepo(t)
	r.write("internal/goal/env.go", `package goal

import "os"

var a = os.Getenv("FOO_URL")
var b, _ = os.LookupEnv("BAR")
var c = os.Getenv("GOAL_URL")
var d = os.Getenv("NEW_UI")
var e = os.Getenv(name)
var f = os.Getenv("FOO_URL")
`)
	fs := r.structure()
	wantFindings(t, fs,
		`internal/goal/env.go:5: env var "FOO_URL" is not registered. Add it to the owning concept's env (owner: goal) or to flags in .arch/registry.yaml.`,
		`internal/goal/env.go:6: env var "BAR"`)
	if fs[0].Entry != "concepts[goal].env" {
		t.Fatalf("entry %q", fs[0].Entry)
	}
}

func TestStructurePythonEnv(t *testing.T) {
	r := structureRepo(t)
	r.write("py/app.py", `import os
a = os.environ["P1"]
b = os.environ.get('P2', "x")
c = os.getenv("P3")
d = os.environ['GOAL_URL']
e = os.getenv(name)
`)
	wantFindings(t, r.structure(), `py/app.py:2: env var "P1"`, `py/app.py:3: env var "P2"`, `py/app.py:4: env var "P3"`)
}

func TestStructureJSEnv(t *testing.T) {
	r := structureRepo(t)
	r.write("web/app.ts", `const a = process.env.J1;
const b = process.env["J2"];
const c = process.env['J3'] || process.env.NEW_UI;
const d = process.env[key];
`)
	r.write("web/other.mjs", "export const x = process.env.J4;\n")
	wantFindings(t, r.structure(),
		`web/app.ts:1: env var "J1"`, `web/app.ts:2: env var "J2"`, `web/app.ts:3: env var "J3"`,
		`web/other.mjs:1: env var "J4"`)
}

func TestStructureLanguageByExtension(t *testing.T) {
	r := structureRepo(t)
	// A JS pattern in a Go file and a Go pattern in a text file do not fire.
	r.write("internal/goal/x.go", "package goal\n\n// process.env.NOPE\n")
	r.write("internal/goal/notes.txt", "os.Getenv(\"NOPE\")\n")
	wantFindings(t, r.structure())
}

func TestStructureTables(t *testing.T) {
	r := structureRepo(t)
	r.write("db/schema.sql", `create table if not exists "Jobs" (id int);
CREATE TABLE goals (id int);
CREATE TABLE IF NOT EXISTS [GOALS] (id int);
CREATE TABLE audit.events (id int);
CREATE TABLE other.goals (id int);
CREATE TABLE `+"`audit`.`log`"+` (id int);
CREATE TABLE jobs (id int);
`)
	fs := r.structure()
	wantFindings(t, fs,
		`db/schema.sql:1: new table "jobs" is not in any concept's storage. Add it to the owning concept's storage in .arch/registry.yaml (owner of this file: goal), or reuse an existing table.`,
		`db/schema.sql:6: new table "audit.log"`)
	if fs[0].Entry != "concepts[goal].storage" {
		t.Fatalf("entry %q", fs[0].Entry)
	}
}

func TestStructureTablesInAnyFile(t *testing.T) {
	r := structureRepo(t)
	r.write("internal/goal/migrate.go", "package goal\n\nconst q = `CREATE TABLE runs (id int)`\n")
	wantFindings(t, r.structure(), `internal/goal/migrate.go:3: new table "runs"`)
}

func TestStructureUnownedFile(t *testing.T) {
	r := structureRepo(t)
	r.write("legacy/x.go", "package legacy\n\nimport \"os\"\n\nvar a = os.Getenv(\"L\")\n")
	fs := r.structure()
	wantFindings(t, fs, `legacy/x.go:5: env var "L" is not registered`)
	if fs[0].Entry != "concepts" || !strings.Contains(fs[0].Message, "no owner") {
		t.Fatalf("got %+v", fs[0])
	}
}

func TestStructureIgnoredFilesSkipped(t *testing.T) {
	r := structureRepo(t)
	r.write("internal/goal/env_test.go", "package goal\n\nvar a = os.Getenv(\"T\")\nconst q = \"CREATE TABLE t (id int)\"\n")
	r.write("py/app_test.py", "os.environ['T']\n")
	wantFindings(t, r.structure())
}

func TestStructureRemovedLinesDoNotFire(t *testing.T) {
	r := structureRepo(t)
	r.write("internal/goal/env.go", "package goal\n\nimport \"os\"\n\nvar a = os.Getenv(\"OLD\")\n")
	r.write("db/schema.sql", "CREATE TABLE old_t (id int);\n")
	r.commitAll("old")
	r.write("internal/goal/env.go", "package goal\n\nimport \"os\"\n\nvar b = 2\n")
	r.write("db/schema.sql", "-- nothing\n")
	wantFindings(t, r.structure())
}
