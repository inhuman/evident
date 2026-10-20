// Package evident judges whether a requirement is proven by observed facts.
//
// A Requirement is split into classes of inputs by whoever owns the spec, not by
// whoever writes the code. Code and test harness record Observations; Judge
// decides. A requirement is OK only when every class was observed and every
// observation satisfied its class predicate. Testing one easy path yields
// Incomplete, not a green check.
//
// A Requirement may also carry a finite Domain (typically exported from a Lean
// model, see package evidentlean): then every row of the domain must be observed
// on the live code, and an observation outside the domain is a violation.
package evident

import (
	"cmp"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Source tells who produced an observation. Implementation-reported facts are
// self-report; harness facts are measured from outside (response headers, file
// contents, DNS answers) and cannot be faked by the code under test.
type Source string

const (
	Impl    Source = "impl"
	Harness Source = "harness"
)

type Observation struct {
	Spec   string `json:"spec"`
	Source Source `json:"source"`
	Facts  Facts  `json:"facts"`
	Test   string `json:"test,omitzero"`
	// Where is file:line of the Witness call. Harness facts recorded outside a
	// _test.go file are rejected: production code must not vouch as harness.
	Where string `json:"where,omitzero"`
}

// Class is one partition of the requirement's input space.
type Class struct {
	Name string
	// Source, when set, counts only observations from that source. Use Harness
	// for facts the implementation must not be able to vouch for itself.
	Source Source
	// When selects observations belonging to this class.
	When func(Facts) bool
	// Then must hold for every observation in the class. Nil means the class
	// only has to be observed.
	Then func(Facts) error
	// Require lists fact keys every observation of the class must carry. A
	// predicate over a missing key reads a zero value and proves nothing.
	Require []string
}

type Requirement struct {
	ID      string
	Text    string
	Classes []Class
	// Domain, when set, is the complete finite input space: every row must be
	// observed, and observations of this spec must fall inside it.
	Domain *Domain
}

// Domain is a finite set of input rows over Keys.
type Domain struct {
	Keys []string
	Rows []Facts
}

type Status string

const (
	OK         Status = "ok"
	Incomplete Status = "incomplete"
	Violated   Status = "violated"
)

type Violation struct {
	Class string `json:"class,omitzero"`
	Facts Facts  `json:"facts"`
	Err   string `json:"err"`
	Where string `json:"where,omitzero"`
}

type ClassResult struct {
	Name     string `json:"name"`
	Observed int    `json:"observed"`
}

type DomainResult struct {
	Rows    int     `json:"rows"`
	Covered int     `json:"covered"`
	Missing []Facts `json:"missing,omitzero"` // first few uncovered rows
}

type Verdict struct {
	Requirement string        `json:"requirement"`
	Status      Status        `json:"status"`
	Classes     []ClassResult `json:"classes"`
	Domain      *DomainResult `json:"domain,omitzero"`
	Violations  []Violation   `json:"violations,omitzero"`
}

const missingSample = 5

// Judge evaluates one requirement against all observations. Observations of
// other specs are ignored. An observation of this spec that matches no class is
// a violation: the partition has a hole the spec owner must close.
func Judge(req Requirement, obs []Observation) Verdict {
	v := Verdict{Requirement: req.ID, Classes: make([]ClassResult, len(req.Classes))}
	if problems := req.validate(); len(problems) > 0 {
		v.Violations = problems
		v.Status = Violated
		return v
	}
	for i, c := range req.Classes {
		v.Classes[i].Name = c.Name
	}

	var dom *domainIndex
	if req.Domain != nil {
		dom = newDomainIndex(req.Domain)
	}

	for _, o := range obs {
		if o.Spec != req.ID {
			continue
		}
		viol := func(class, msg string) {
			v.Violations = append(v.Violations, Violation{Class: class, Facts: o.Facts, Err: msg, Where: o.Where})
		}
		if o.Source == Harness && !strings.HasSuffix(fileOf(o.Where), "_test.go") {
			viol("", fmt.Sprintf("harness fact recorded outside a _test.go file (%q)", o.Where))
			continue
		}
		if dom != nil {
			if err := dom.cover(o.Facts); err != nil {
				viol("", err.Error())
				continue
			}
		}

		matched := false
		for i, c := range req.Classes {
			if c.Source != "" && c.Source != o.Source {
				continue
			}
			if !c.When(o.Facts) {
				continue
			}
			matched = true
			v.Classes[i].Observed++
			if missing := missingKeys(o.Facts, c.Require); len(missing) > 0 {
				viol(c.Name, fmt.Sprintf("missing required facts %v", missing))
				continue
			}
			if c.Then == nil {
				continue
			}
			if err := c.Then(o.Facts); err != nil {
				viol(c.Name, err.Error())
			}
		}
		if !matched {
			viol("", fmt.Sprintf("unclassified %s observation", o.Source))
		}
	}

	if dom != nil {
		v.Domain = dom.result()
	}
	switch {
	case len(v.Violations) > 0:
		v.Status = Violated
	case slices.ContainsFunc(v.Classes, func(c ClassResult) bool { return c.Observed == 0 }):
		v.Status = Incomplete
	case v.Domain != nil && v.Domain.Covered < v.Domain.Rows:
		v.Status = Incomplete
	default:
		v.Status = OK
	}
	return v
}

func (req Requirement) validate() []Violation {
	var out []Violation
	if req.ID == "" {
		out = append(out, Violation{Err: "requirement has no ID"})
	}
	if len(req.Classes) == 0 {
		out = append(out, Violation{Err: "requirement declares no classes"})
	}
	seen := map[string]bool{}
	for _, c := range req.Classes {
		if c.When == nil {
			out = append(out, Violation{Class: c.Name, Err: "class has no When selector"})
		}
		if seen[c.Name] {
			out = append(out, Violation{Class: c.Name, Err: "duplicate class name"})
		}
		seen[c.Name] = true
	}
	if req.Domain != nil && (len(req.Domain.Keys) == 0 || len(req.Domain.Rows) == 0) {
		out = append(out, Violation{Err: "domain has no keys or no rows"})
	}
	return out
}

func missingKeys(f Facts, keys []string) []string {
	var out []string
	for _, k := range keys {
		if _, ok := f[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

type domainIndex struct {
	keys    []string
	rows    map[string]Facts
	order   []string
	covered map[string]bool
}

func newDomainIndex(d *Domain) *domainIndex {
	idx := &domainIndex{keys: d.Keys, rows: map[string]Facts{}, covered: map[string]bool{}}
	for _, r := range d.Rows {
		k := RowKey(d.Keys, r)
		if _, dup := idx.rows[k]; !dup {
			idx.order = append(idx.order, k)
		}
		idx.rows[k] = r
	}
	return idx
}

func (d *domainIndex) cover(f Facts) error {
	if missing := missingKeys(f, d.keys); len(missing) > 0 {
		return fmt.Errorf("observation lacks domain inputs %v", missing)
	}
	k := RowKey(d.keys, f)
	if _, ok := d.rows[k]; !ok {
		return fmt.Errorf("observation outside the domain: %s", k)
	}
	d.covered[k] = true
	return nil
}

func (d *domainIndex) result() *DomainResult {
	r := &DomainResult{Rows: len(d.order), Covered: len(d.covered)}
	for _, k := range d.order {
		if !d.covered[k] && len(r.Missing) < missingSample {
			r.Missing = append(r.Missing, d.rows[k])
		}
	}
	return r
}

// RowKey is the canonical encoding of f restricted to keys. Numbers of any Go
// type encode alike (int 3, int64 3 and float64 3 are the same row).
func RowKey(keys []string, f Facts) string {
	vals := make([]any, len(keys))
	for i, k := range keys {
		vals[i] = normalize(f[k])
	}
	b, err := json.Marshal(vals)
	if err != nil {
		return fmt.Sprintf("%v", vals)
	}
	return string(b)
}

func fileOf(where string) string {
	if i := strings.LastIndexByte(where, ':'); i >= 0 {
		return where[:i]
	}
	return where
}

func (v Verdict) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", v.Requirement, v.Status)
	for _, c := range v.Classes {
		mark := "✓"
		if c.Observed == 0 {
			mark = "∅"
		}
		fmt.Fprintf(&b, "  %s %-24s observed=%d\n", mark, c.Name, c.Observed)
	}
	if v.Domain != nil {
		mark := "✓"
		if v.Domain.Covered < v.Domain.Rows {
			mark = "∅"
		}
		fmt.Fprintf(&b, "  %s domain %d/%d rows observed\n", mark, v.Domain.Covered, v.Domain.Rows)
		for _, m := range v.Domain.Missing {
			fmt.Fprintf(&b, "      missing %v\n", map[string]any(m))
		}
	}
	for _, viol := range v.Violations {
		class := cmp.Or(viol.Class, "<unclassified>")
		fmt.Fprintf(&b, "  ✗ %s: %s facts=%v", class, viol.Err, map[string]any(viol.Facts))
		if viol.Where != "" {
			fmt.Fprintf(&b, " at %s", viol.Where)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Ledger collects observations; safe for concurrent use.
type Ledger struct {
	mu  sync.Mutex
	obs []Observation
}

// Record stores an implementation observation. Harness facts must go through
// Witness, which records where they came from; Record panics on them.
func (l *Ledger) Record(o Observation) {
	if o.Source == Harness {
		panic("evident: harness facts must be recorded with Witness")
	}
	l.add(o)
}

// Witness records a harness-measured fact, stamped with the caller's file:line.
func (l *Ledger) Witness(spec string, facts Facts) {
	l.add(Observation{Spec: spec, Source: Harness, Facts: facts, Where: caller(2)})
}

func (l *Ledger) add(o Observation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.obs = append(l.obs, o)
}

func (l *Ledger) Observations() []Observation {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.obs)
}

func caller(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s:%d", file, line)
}
