// Package evident judges whether a requirement is proven by observed facts.
//
// A Requirement is split into classes of inputs by whoever owns the spec, not by
// whoever writes the code. Code and test harness record Observations; Judge
// decides. A requirement is OK only when every class was observed and every
// observation satisfied its class predicate. Testing one easy path yields
// Incomplete, not a green check.
package evident

import (
	"cmp"
	"fmt"
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
}

type Requirement struct {
	ID      string
	Text    string
	Classes []Class
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
}

type ClassResult struct {
	Name     string `json:"name"`
	Observed int    `json:"observed"`
}

type Verdict struct {
	Requirement string        `json:"requirement"`
	Status      Status        `json:"status"`
	Classes     []ClassResult `json:"classes"`
	Violations  []Violation   `json:"violations,omitzero"`
}

// Judge evaluates one requirement against all observations. Observations of
// other specs are ignored. An observation of this spec that matches no class is
// a violation: the partition has a hole the spec owner must close.
func Judge(req Requirement, obs []Observation) Verdict {
	v := Verdict{Requirement: req.ID, Classes: make([]ClassResult, len(req.Classes))}
	if len(req.Classes) == 0 {
		v.Violations = append(v.Violations, Violation{Err: "requirement declares no classes"})
	}
	for i, c := range req.Classes {
		v.Classes[i].Name = c.Name
		if c.When == nil {
			v.Violations = append(v.Violations, Violation{Class: c.Name, Err: "class has no When selector"})
		}
	}
	if len(v.Violations) > 0 {
		v.Status = Violated
		return v
	}

	for _, o := range obs {
		if o.Spec != req.ID {
			continue
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
			if c.Then == nil {
				continue
			}
			if err := c.Then(o.Facts); err != nil {
				v.Violations = append(v.Violations, Violation{Class: c.Name, Facts: o.Facts, Err: err.Error()})
			}
		}
		if !matched {
			v.Violations = append(v.Violations, Violation{Facts: o.Facts, Err: fmt.Sprintf("unclassified %s observation", o.Source)})
		}
	}

	switch {
	case len(v.Violations) > 0:
		v.Status = Violated
	case slices.ContainsFunc(v.Classes, func(c ClassResult) bool { return c.Observed == 0 }):
		v.Status = Incomplete
	default:
		v.Status = OK
	}
	return v
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
	for _, viol := range v.Violations {
		class := cmp.Or(viol.Class, "<unclassified>")
		fmt.Fprintf(&b, "  ✗ %s: %s facts=%v\n", class, viol.Err, map[string]any(viol.Facts))
	}
	return b.String()
}

// Ledger collects observations; safe for concurrent use.
type Ledger struct {
	mu  sync.Mutex
	obs []Observation
}

func (l *Ledger) Record(o Observation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.obs = append(l.obs, o)
}

// Witness records a harness-measured fact.
func (l *Ledger) Witness(spec string, facts Facts) {
	l.Record(Observation{Spec: spec, Source: Harness, Facts: facts})
}

func (l *Ledger) Observations() []Observation {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.obs)
}
