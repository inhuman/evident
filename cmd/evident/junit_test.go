package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inhuman/evident"
	"github.com/inhuman/evident/evidenttest"
)

func parseReport(t *testing.T, data []byte) map[string]junitSuite {
	t.Helper()
	var r junitSuites
	if err := xml.Unmarshal(data, &r); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	out := map[string]junitSuite{}
	for _, s := range r.Suites {
		out[s.Name] = s
	}
	return out
}

func caseOf(t *testing.T, s junitSuite, name string) junitCase {
	t.Helper()
	for _, c := range s.Cases {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("suite %s has no case %q: %+v", s.Name, name, s.Cases)
	return junitCase{}
}

func TestJUnitRequirements(t *testing.T) {
	ok := evident.Verdict{Requirement: "a", Status: evident.OK, Classes: []evident.ClassResult{{Name: "x", Observed: 2}},
		Domain: &evident.DomainResult{Rows: 2, Covered: 2}}
	bad := evident.Verdict{Requirement: "b", Status: evident.Violated,
		Classes: []evident.ClassResult{{Name: "held", Observed: 1}, {Name: "broken", Observed: 1}, {Name: "unseen"}},
		Domain:  &evident.DomainResult{Rows: 3, Covered: 2, Missing: []evident.Facts{{"in": 3}}},
		Violations: []evident.Violation{
			{Class: "broken", Err: "status = 500, want 200", Facts: evident.Facts{"status": 500}, Where: "x_test.go:9"},
			{Err: "matches no class", Facts: evident.Facts{"odd": true}},
		}}
	path := writeLines(t,
		evidenttest.Line{Kind: "registry", Registry: []string{"a", "b", "never"}},
		evidenttest.Line{Kind: "verdict", Verdict: &ok},
		evidenttest.Line{Kind: "verdict", Verdict: &bad},
		evidenttest.Line{Kind: "verdict", Verdict: &evident.Verdict{Requirement: "stray", Status: evident.OK}},
	)
	var buf bytes.Buffer
	if err := run([]string{"junit", path}, &buf); err != nil {
		t.Fatal(err)
	}
	suites := parseReport(t, buf.Bytes())

	if s := suites["a"]; s.Failures != 0 || s.Tests != 2 {
		t.Errorf("ok requirement: %+v", s)
	}
	b := suites["b"]
	if c := caseOf(t, b, "held"); c.Failure != nil {
		t.Errorf("held class failed: %+v", c.Failure)
	}
	if c := caseOf(t, b, "broken"); c.Failure == nil || !strings.Contains(c.Failure.Text, "status = 500") ||
		!strings.Contains(c.Failure.Text, "x_test.go:9") {
		t.Errorf("violated class: %+v", c.Failure)
	}
	if c := caseOf(t, b, "unseen"); c.Failure == nil || c.Failure.Message != "never observed" {
		t.Errorf("unobserved class: %+v", c.Failure)
	}
	if c := caseOf(t, b, "unclassified observations"); c.Failure == nil {
		t.Error("unclassified observation must fail")
	}
	if c := caseOf(t, b, "domain"); c.Failure == nil || c.Failure.Message != "2/3 rows observed" {
		t.Errorf("domain: %+v", c.Failure)
	}
	if b.Failures != 4 {
		t.Errorf("b failures = %d, want 4", b.Failures)
	}
	if c := caseOf(t, suites["never"], "judged"); c.Failure == nil {
		t.Error("registered but never judged must fail")
	}
	if c := caseOf(t, suites["stray"], "registered"); c.Failure == nil {
		t.Error("judged but not registered must fail")
	}
}

func TestJUnitLeanModels(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "evident.lock")
	model := "../../testdata/gate.json"
	if err := run([]string{"lock", lockPath, model}, io.Discard); err != nil {
		t.Fatal(err)
	}
	verdicts := writeLines(t, evidenttest.Line{Kind: "registry", Registry: []string{}})
	out := filepath.Join(t.TempDir(), "report.xml")
	if err := run([]string{"junit", "-o", out, "-lock", lockPath, verdicts, model}, io.Discard); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, out)
	s := parseReport(t, data)["lean: test.gate"]
	if s.Tests < 2 || s.Failures != 0 {
		t.Fatalf("lean suite: %+v", s)
	}
	if c := caseOf(t, s, "oracle table"); !strings.Contains(c.SystemOut, "rows") {
		t.Errorf("oracle table case: %+v", c)
	}

	// An unlocked model shows as failures, not as a silent pass.
	if err := run([]string{"junit", "-o", out, "-lock", filepath.Join(t.TempDir(), "missing.lock"), verdicts, model}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if s := parseReport(t, readFile(t, out))["lean: test.gate"]; s.Failures != s.Tests {
		t.Errorf("unlocked model: %d of %d cases failed", s.Failures, s.Tests)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
