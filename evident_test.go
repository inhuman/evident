package evident

import (
	"errors"
	"strings"
	"testing"
)

var corsReq = Requirement{
	ID: "cors.allowlist",
	Classes: []Class{
		{Name: "allowed", When: Is("decision", "allow"), Then: Expect("header_set", true)},
		{Name: "denied", When: Is("decision", "deny"), Then: Expect("header_set", false)},
	},
}

func obs(spec string, src Source, f Facts) Observation {
	return Observation{Spec: spec, Source: src, Facts: f}
}

func TestJudge(t *testing.T) {
	allow := obs("cors.allowlist", Impl, Facts{"decision": "allow", "header_set": true})
	deny := obs("cors.allowlist", Impl, Facts{"decision": "deny", "header_set": false})

	tests := []struct {
		name       string
		req        Requirement
		obs        []Observation
		want       Status
		violations int
		observed   []int
	}{
		{name: "all classes proven", req: corsReq, obs: []Observation{allow, deny}, want: OK, observed: []int{1, 1}},
		{name: "one easy path only", req: corsReq, obs: []Observation{allow, allow}, want: Incomplete, observed: []int{2, 0}},
		{name: "nothing observed", req: corsReq, want: Incomplete, observed: []int{0, 0}},
		{
			name:       "predicate broken",
			req:        corsReq,
			obs:        []Observation{allow, obs("cors.allowlist", Impl, Facts{"decision": "deny", "header_set": true})},
			want:       Violated,
			violations: 1,
			observed:   []int{1, 1},
		},
		{
			name:       "observation outside partition",
			req:        corsReq,
			obs:        []Observation{allow, deny, obs("cors.allowlist", Impl, Facts{"decision": "maybe"})},
			want:       Violated,
			violations: 1,
			observed:   []int{1, 1},
		},
		{
			name:     "other specs ignored",
			req:      corsReq,
			obs:      []Observation{allow, deny, obs("other", Impl, Facts{"decision": "maybe"})},
			want:     OK,
			observed: []int{1, 1},
		},
		{name: "no classes", req: Requirement{ID: "x"}, want: Violated, violations: 1, observed: []int{}},
		{
			name:       "class without selector",
			req:        Requirement{ID: "x", Classes: []Class{{Name: "a"}}},
			obs:        []Observation{obs("x", Impl, Facts{})},
			want:       Violated,
			violations: 1,
			observed:   []int{0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Judge(tt.req, tt.obs)
			if v.Status != tt.want {
				t.Fatalf("status = %s, want %s\n%s", v.Status, tt.want, v)
			}
			if len(v.Violations) != tt.violations {
				t.Fatalf("violations = %d, want %d\n%s", len(v.Violations), tt.violations, v)
			}
			for i, n := range tt.observed {
				if v.Classes[i].Observed != n {
					t.Fatalf("class %s observed = %d, want %d", v.Classes[i].Name, v.Classes[i].Observed, n)
				}
			}
		})
	}
}

func TestJudgeHarnessOnlyClassIgnoresSelfReport(t *testing.T) {
	req := Requirement{
		ID: "hosts.removed",
		Classes: []Class{{
			Name:   "deleted entry gone from file",
			Source: Harness,
			When:   Is("op", "delete"),
			Then:   Expect("present_in_file", false),
		}},
	}
	self := obs("hosts.removed", Impl, Facts{"op": "delete", "present_in_file": false})

	v := Judge(req, []Observation{self})
	if v.Status != Violated {
		t.Fatalf("self-report must not satisfy a harness class; got %s\n%s", v.Status, v)
	}

	var l Ledger
	l.Witness("hosts.removed", Facts{"op": "delete", "present_in_file": false})
	if v := Judge(req, l.Observations()); v.Status != OK {
		t.Fatalf("harness witness should prove it; got %s\n%s", v.Status, v)
	}
}

func TestJudgeOverlappingClassesBothCount(t *testing.T) {
	req := Requirement{
		ID: "x",
		Classes: []Class{
			{Name: "any", When: func(Facts) bool { return true }},
			{Name: "strict", When: Is("k", "v"), Then: func(Facts) error { return errors.New("boom") }},
		},
	}
	v := Judge(req, []Observation{obs("x", Impl, Facts{"k": "v"})})
	if v.Status != Violated || v.Classes[0].Observed != 1 || v.Classes[1].Observed != 1 {
		t.Fatalf("unexpected verdict\n%s", v)
	}
}

func TestVerdictStringMarksUnobservedAndViolations(t *testing.T) {
	v := Judge(corsReq, []Observation{obs("cors.allowlist", Impl, Facts{"decision": "allow", "header_set": false})})
	s := v.String()
	for _, want := range []string{"cors.allowlist: violated", "∅ denied", "✗ allowed: header_set = false, want true"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q:\n%s", want, s)
		}
	}
}

func TestFactsAccessors(t *testing.T) {
	f := Facts{"s": "x", "b": true, "i": int64(3), "f": float64(4), "l": []any{"a", "b"}, "ls": []string{"c"}}
	if f.String("s") != "x" || !f.Bool("b") || f.Int("i") != 3 || f.Int("f") != 4 {
		t.Fatalf("scalar accessors broken: %v", f)
	}
	if got := f.Strings("l"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("Strings([]any) = %v", got)
	}
	if got := f.Strings("ls"); len(got) != 1 || got[0] != "c" {
		t.Fatalf("Strings([]string) = %v", got)
	}
	if f.Has("missing") || f.String("missing") != "" || f.Int("missing") != 0 {
		t.Fatal("missing key must read as zero")
	}
}
