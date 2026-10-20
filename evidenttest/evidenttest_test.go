package evidenttest

import (
	"bytes"
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

func TestAppendJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.jsonl")
	v := evident.Judge(req, nil)
	for range 2 {
		if err := appendJSONL(path, []evident.Verdict{v}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	n := 0
	for dec.More() {
		var got evident.Verdict
		if err := dec.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Status != evident.Incomplete || got.Requirement != "r" {
			t.Fatalf("decoded %+v", got)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("lines = %d, want 2 (append, not overwrite)", n)
	}
}
