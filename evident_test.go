package evident

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var corsReq = Requirement{
	ID: "cors.allowlist",
	Classes: []Class{
		{Name: "allowed", When: Is("decision", "allow"), Then: Expect("header_set", true)},
		{Name: "denied", When: Is("decision", "deny"), Then: Expect("header_set", false)},
	},
}

func obs(spec string, src Source, f Facts) Observation {
	o := Observation{Spec: spec, Source: src, Facts: f}
	if src == Harness {
		o.Where = "some_test.go:1"
	}
	return o
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
		{
			name:       "duplicate class names",
			req:        Requirement{ID: "x", Classes: []Class{{Name: "a", When: Always}, {Name: "a", When: Always}}},
			want:       Violated,
			violations: 1,
			observed:   []int{0, 0},
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

func TestHarnessFactMustComeFromATestFile(t *testing.T) {
	req := Requirement{ID: "x", Classes: []Class{{Name: "any", When: Always}}}
	forged := Observation{Spec: "x", Source: Harness, Facts: Facts{}, Where: "/app/internal/forwarder.go:42"}
	v := Judge(req, []Observation{forged})
	if v.Status != Violated || !strings.Contains(v.Violations[0].Err, "outside a _test.go") {
		t.Fatalf("harness fact from production code accepted\n%s", v)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("Record must refuse harness facts: only Witness stamps their origin")
		}
	}()
	var l Ledger
	l.Record(Observation{Spec: "x", Source: Harness, Where: "fake_test.go:1"})
}

func TestRequiredFactsMustBePresent(t *testing.T) {
	req := Requirement{ID: "m", Classes: []Class{{
		Name:    "migrated",
		When:    Is("case", "rows"),
		Then:    func(f Facts) error { return Expect("got", f.String("want"))(f) },
		Require: []string{"got", "want"},
	}}}
	v := Judge(req, []Observation{obs("m", Harness, Facts{"case": "rows"})})
	if v.Status != Violated || !strings.Contains(v.Violations[0].Err, "missing required facts") {
		t.Fatalf("a class without its facts must not pass\n%s", v)
	}
}

func TestDomainCoverage(t *testing.T) {
	req := Requirement{
		ID:      "d",
		Classes: []Class{{Name: "oracle", When: Always, Then: func(f Facts) error { return Expect("out", f.Bool("a") && f.Bool("b"))(f) }}},
		Domain: &Domain{Keys: []string{"a", "b"}, Rows: []Facts{
			{"a": false, "b": false}, {"a": false, "b": true}, {"a": true, "b": false}, {"a": true, "b": true},
		}},
	}
	row := func(a, b, out bool) Observation { return obs("d", Harness, Facts{"a": a, "b": b, "out": out}) }

	partial := Judge(req, []Observation{row(true, true, true), row(false, false, false)})
	if partial.Status != Incomplete || partial.Domain.Covered != 2 || len(partial.Domain.Missing) != 2 {
		t.Fatalf("2 of 4 rows must be incomplete\n%s", partial)
	}

	full := Judge(req, []Observation{row(true, true, true), row(false, false, false), row(true, false, false), row(false, true, false)})
	if full.Status != OK {
		t.Fatalf("all rows observed must be ok\n%s", full)
	}

	outside := Judge(req, []Observation{obs("d", Harness, Facts{"a": "typo", "b": true, "out": false})})
	if outside.Status != Violated || !strings.Contains(outside.Violations[0].Err, "outside the domain") {
		t.Fatalf("observation outside the domain must be a violation\n%s", outside)
	}
}

func TestDomainRowsMatchAcrossNumericTypes(t *testing.T) {
	req := Requirement{
		ID:      "n",
		Classes: []Class{{Name: "any", When: Always}},
		Domain:  &Domain{Keys: []string{"code"}, Rows: []Facts{{"code": float64(200)}}},
	}
	if v := Judge(req, []Observation{obs("n", Harness, Facts{"code": int64(200)})}); v.Status != OK {
		t.Fatalf("int64 200 must match a JSON 200 row\n%s", v)
	}
}

func TestOverlappingClassesBothCount(t *testing.T) {
	req := Requirement{
		ID: "x",
		Classes: []Class{
			{Name: "any", When: Always},
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

func TestEqualAndIsAcrossTypes(t *testing.T) {
	cases := []struct {
		a, b any
		want bool
	}{
		{200, int64(200), true},
		{uint16(200), float64(200), true},
		{float32(1.5), 1.5, true},
		{200, "200", false},
		{[]string{"a"}, []any{"a"}, true},
		{[]string{"a"}, []string{"b"}, false},
		{nil, nil, true},
		{map[string]any{"reply": "corp", "asked": []string{"corp1", "embedded"}, "n": 2},
			map[string]any{"reply": "corp", "asked": []any{"corp1", "embedded"}, "n": float64(2)}, true},
		{Facts{"a": []string{"x"}}, map[string]any{"a": []any{"x"}}, true},
		{map[string]any{"asked": []string{"embedded", "corp1"}}, map[string]any{"asked": []any{"corp1", "embedded"}}, false},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("Equal(%#v, %#v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if !Is("status", 200)(Facts{"status": int64(200)}) {
		t.Error("Is(200) must match int64 200")
	}
	if err := Expect("hosts", []string{"a"})(Facts{"hosts": []any{"a"}}); err != nil {
		t.Errorf("Expect on slices: %v", err)
	}
	if err := Expect("x", false)(Facts{}); err == nil {
		t.Error("Expect must fail on a missing key even when want is the zero value")
	}
}

func TestFactsAccessors(t *testing.T) {
	f := Facts{
		"s": "x", "b": true, "i": int64(3), "f": float64(4), "u": uint64(9000), "d": 9 * time.Second,
		"l": []any{"a", "b"}, "ls": []string{"c"},
	}
	if f.String("s") != "x" || !f.Bool("b") || f.Int("i") != 3 || f.Int("f") != 4 || f.Int("u") != 9000 {
		t.Fatalf("scalar accessors broken: %v", f)
	}
	if f.Int("d") != int64(9*time.Second) {
		t.Fatalf("Duration must read as nanoseconds, got %d", f.Int("d"))
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
