package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inhuman/evident"
	"github.com/inhuman/evident/evidenttest"
)

func writeLines(t *testing.T, lines ...evidenttest.Line) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func verdict(id string, s evident.Status, filtered, strict bool) evidenttest.Line {
	return evidenttest.Line{Kind: "verdict", Verdict: &evident.Verdict{Requirement: id, Status: s}, Filtered: filtered, Strict: strict}
}

func TestGate(t *testing.T) {
	registry := evidenttest.Line{Kind: "registry", Registry: []string{"a", "b"}}
	for name, c := range map[string]struct {
		lines []evidenttest.Line
		want  string // empty: must pass
	}{
		"all ok":              {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false), verdict("b", evident.OK, false, false)}, ""},
		"never judged":        {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false)}, "b: registered but never judged"},
		"incomplete":          {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false), verdict("b", evident.Incomplete, false, false)}, "b: incomplete"},
		"filtered not strict": {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false), verdict("b", evident.OK, true, false)}, "filtered"},
		"filtered but strict": {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false), verdict("b", evident.OK, true, true)}, ""},
		"unregistered":        {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false), verdict("b", evident.OK, false, false), verdict("c", evident.OK, false, false)}, "c: judged but not registered"},
		"no registry":         {[]evidenttest.Line{verdict("a", evident.OK, false, false)}, "no registry line"},
		"last verdict wins":   {[]evidenttest.Line{registry, verdict("a", evident.OK, false, false), verdict("b", evident.OK, false, false), verdict("b", evident.Violated, false, false)}, "b: violated"},
	} {
		err := gate(writeLines(t, c.lines...), io.Discard)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: gate failed: %v", name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: gate = %v, want error containing %q", name, err, c.want)
		}
	}
}

func TestLockThenVerify(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "evident.lock.json")
	model := "../../testdata/gate.json"
	if err := run([]string{"verify", lockPath, model}, io.Discard); err == nil {
		t.Fatal("verify without a lock entry must fail")
	}
	if err := run([]string{"lock", lockPath, model}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", lockPath, model}, io.Discard); err != nil {
		t.Fatalf("verify after lock: %v", err)
	}
}
