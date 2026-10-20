package evidenttest

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/inhuman/evident"
)

type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper()               {}
func (r *recorder) Errorf(string, ...any) { r.failed = true }
func (r *recorder) Logf(string, ...any)   {}

var req = evident.Requirement{
	ID: "r",
	Classes: []evident.Class{
		{Name: "a", When: evident.Is("k", "a")},
		{Name: "b", When: evident.Is("k", "b")},
	},
}

func TestCheckFailsOnIncomplete(t *testing.T) {
	var l evident.Ledger
	l.Witness("r", evident.Facts{"k": "a"})

	r := &recorder{TB: t}
	Check(r, &l, req)
	if !r.failed {
		t.Fatal("Check passed with class b unobserved")
	}

	l.Witness("r", evident.Facts{"k": "b"})
	r = &recorder{TB: t}
	Check(r, &l, req)
	if r.failed {
		t.Fatal("Check failed with every class observed")
	}
}

func readLines(t *testing.T, path string) []Line {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func TestLinesCarryRunMetadataAndAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.jsonl")
	t.Setenv(OutEnv, path)
	t.Setenv("CI_COMMIT_SHA", "abc123")
	t.Setenv("CI_PIPELINE_ID", "42")

	v := evident.Judge(req, nil)
	for range 2 {
		if err := appendLines([]Line{meta(Line{Kind: "verdict", Verdict: &v, Filtered: true})}); err != nil {
			t.Fatal(err)
		}
	}
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (append, not overwrite)", len(lines))
	}
	l := lines[1]
	if l.Verdict.Status != evident.Incomplete || l.Commit != "abc123" || l.Run != "42" || !l.Filtered {
		t.Fatalf("line = %+v", l)
	}
}

func TestRelativeOutIsFromModuleRoot(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(pkg)
	t.Setenv(OutEnv, "v.jsonl")

	v := evident.Judge(req, nil)
	if err := appendLines([]Line{{Kind: "verdict", Verdict: &v}}); err != nil {
		t.Fatal(err)
	}
	if len(readLines(t, filepath.Join(root, "v.jsonl"))) != 1 {
		t.Fatal("relative EVIDENT_OUT must land next to go.mod")
	}
	if _, err := os.Stat(filepath.Join(pkg, "v.jsonl")); err == nil {
		t.Fatal("relative EVIDENT_OUT must not land in the package directory")
	}
}

func TestRegistryRecordsIDsAndRejectsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.jsonl")
	t.Setenv(OutEnv, path)

	r := &recorder{TB: t}
	Registry(r, req, evident.Requirement{ID: "other"})
	if r.failed {
		t.Fatal("unique IDs must register")
	}
	lines := readLines(t, path)
	if len(lines) != 1 || lines[0].Kind != "registry" || len(lines[0].Registry) != 2 {
		t.Fatalf("registry line = %+v", lines)
	}

	r = &recorder{TB: t}
	Registry(r, req, req)
	if !r.failed {
		t.Fatal("duplicate IDs must fail the registry")
	}
}
