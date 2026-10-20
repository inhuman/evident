package evidentlean

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/inhuman/evident"
)

// testdata/gate.json is exported from lean/EvidentTest/Gate.lean by
// `lake env lean EvidentTest/Export.lean`.
func loadGate(t *testing.T) (*Model, []byte) {
	t.Helper()
	data, err := os.ReadFile("../testdata/gate.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return m, data
}

func lockOf(t *testing.T, m *Model) []byte {
	t.Helper()
	b, err := json.Marshal(Lock{m.ID: m.Entry()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// canOpen is the "implementation" under test; broken flips one row.
func canOpen(role string, locked, broken bool) bool {
	if broken && role == "user" && !locked {
		return true
	}
	return role == "admin" && !locked
}

func run(m *Model, broken bool, skip int) []evident.Observation {
	var l evident.Ledger
	for i, r := range m.Rows {
		if i == skip {
			continue
		}
		role, locked := r.In.String("role"), r.In.Bool("locked")
		l.Witness(m.ID, evident.Facts{"role": role, "locked": locked, "open": canOpen(role, locked, broken)})
	}
	return l.Observations()
}

func TestModelRequirement(t *testing.T) {
	m, _ := loadGate(t)
	req := m.Requirement("only an unlocked admin opens")

	if v := evident.Judge(req, run(m, false, -1)); v.Status != evident.OK {
		t.Fatalf("correct implementation over the whole domain must be ok\n%s", v)
	}
	if v := evident.Judge(req, run(m, true, -1)); v.Status != evident.Violated {
		t.Fatalf("a row disagreeing with the model must be violated\n%s", v)
	}
	if v := evident.Judge(req, run(m, false, 4)); v.Status != evident.Incomplete || v.Domain.Covered != 5 {
		t.Fatalf("a skipped row must leave the requirement incomplete\n%s", v)
	}

	var l evident.Ledger
	l.Witness(m.ID, evident.Facts{"role": "admin", "locked": false})
	if v := evident.Judge(req, l.Observations()); v.Status != evident.Violated {
		t.Fatalf("an observation without the output must not pass\n%s", v)
	}
}

func TestLockCatchesSpecChanges(t *testing.T) {
	m, data := loadGate(t)
	lock, err := ParseLock(lockOf(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Verify(m); err != nil {
		t.Fatalf("unchanged model must verify: %v", err)
	}

	edited := func(f func(*Model)) *Model {
		var c Model
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatal(err)
		}
		f(&c)
		return &c
	}
	for name, c := range map[string]struct {
		model *Model
		want  string
	}{
		"hand-edited table": {edited(func(c *Model) { c.Rows[2].Out = true }), "oracle table changed"},
		"weakened theorem": {edited(func(c *Model) {
			c.Theorems[0].Statement = "True"
		}), "reworded"},
		"dropped theorem": {edited(func(c *Model) { c.Theorems = c.Theorems[1:] }), "is gone"},
		"unlocked theorem": {edited(func(c *Model) {
			c.Theorems = append(c.Theorems, Theorem{Name: "extra", Statement: "True"})
		}), "not in the lock"},
		"unknown model": {edited(func(c *Model) { c.ID = "other" }), "not in the lock"},
	} {
		err := lock.Verify(c.model)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Verify = %v, want error containing %q", name, err, c.want)
		}
	}
}

func TestParseRejectsForbiddenAxiomsAndBadRows(t *testing.T) {
	_, data := loadGate(t)
	var c Model
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	c.Theorems[0].Axioms = append(c.Theorems[0].Axioms, "sorryAx")
	bad, _ := json.Marshal(c)
	if _, err := Parse(bad); err == nil || !strings.Contains(err.Error(), "sorryAx") {
		t.Errorf("Parse must reject sorryAx, got %v", err)
	}

	c.Theorems[0].Axioms = nil
	c.Rows = append(c.Rows, c.Rows[0])
	dup, _ := json.Marshal(c)
	if _, err := Parse(dup); err == nil || !strings.Contains(err.Error(), "duplicate row") {
		t.Errorf("Parse must reject duplicate rows, got %v", err)
	}
}

func TestMustRequirementPanicsOnLockMismatch(t *testing.T) {
	m, data := loadGate(t)
	if req := MustRequirement(data, lockOf(t, m), "gate"); req.ID != "test.gate" || req.Domain == nil {
		t.Fatalf("requirement = %+v", req)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unlocked model must panic")
		}
	}()
	MustRequirement(data, []byte(`{}`), "gate")
}
