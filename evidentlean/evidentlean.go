// Package evidentlean turns a Lean model exported with `#evident_export` into
// an evident.Requirement.
//
// The Lean side proves properties of the model (kernel-checked, standard axioms
// only) and enumerates its oracle over the whole finite domain. Here that table
// becomes the requirement's Domain plus an oracle class: the requirement is OK
// only when the harness observed every row on the live code and each observed
// output equals the model's.
//
// A lock file, owned by the spec owner, pins each model: the theorem statements
// and a hash of the table. A weakened theorem, a dropped theorem, or a
// hand-edited JSON table fails Verify, so changing the specification is always
// a visible change of the lock.
package evidentlean

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/inhuman/evident"
)

// AllowedAxioms mirrors Evident.allowedAxioms on the Lean side; it is checked
// again here because the JSON file could be edited by hand.
var AllowedAxioms = []string{"propext", "Classical.choice", "Quot.sound"}

type Theorem struct {
	Name      string   `json:"name"`
	Statement string   `json:"statement"`
	Axioms    []string `json:"axioms"`
}

type Row struct {
	In  evident.Facts `json:"in"`
	Out any           `json:"out"`
}

type Model struct {
	ID       string    `json:"id"`
	Inputs   []string  `json:"inputs"`
	Output   string    `json:"output"`
	Rows     []Row     `json:"rows"`
	Theorems []Theorem `json:"theorems"`
}

// Parse decodes and validates an exported model.
func Parse(data []byte) (*Model, error) {
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode lean model: %w", err)
	}
	if m.ID == "" || len(m.Inputs) == 0 || m.Output == "" || len(m.Rows) == 0 {
		return nil, errors.New("lean model needs id, inputs, output and rows")
	}
	seen := map[string]bool{}
	for i, r := range m.Rows {
		if len(r.In) != len(m.Inputs) {
			return nil, fmt.Errorf("%s: row %d has %d inputs, want %v", m.ID, i, len(r.In), m.Inputs)
		}
		for _, k := range m.Inputs {
			if _, ok := r.In[k]; !ok {
				return nil, fmt.Errorf("%s: row %d lacks input %q", m.ID, i, k)
			}
		}
		key := evident.RowKey(m.Inputs, r.In)
		if seen[key] {
			return nil, fmt.Errorf("%s: duplicate row %s", m.ID, key)
		}
		seen[key] = true
	}
	for _, t := range m.Theorems {
		for _, a := range t.Axioms {
			if !slices.Contains(AllowedAxioms, a) {
				return nil, fmt.Errorf("%s: theorem %s depends on axiom %s", m.ID, t.Name, a)
			}
		}
	}
	return &m, nil
}

// TableHash fingerprints inputs, output and rows independent of JSON layout.
func (m *Model) TableHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%q|%q\n", m.Inputs, m.Output)
	keys := make([]string, 0, len(m.Rows))
	outs := map[string]string{}
	for _, r := range m.Rows {
		k := evident.RowKey(m.Inputs, r.In)
		keys = append(keys, k)
		out, _ := json.Marshal(r.Out)
		outs[k] = string(out)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, outs[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LockEntry pins one model.
type LockEntry struct {
	TableSHA256 string            `json:"table_sha256"`
	Theorems    map[string]string `json:"theorems"`
}

// Lock maps model IDs to their pinned entry.
type Lock map[string]LockEntry

func ParseLock(data []byte) (Lock, error) {
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("decode lock: %w", err)
	}
	return l, nil
}

// Entry is the lock entry that pins m as it is now.
func (m *Model) Entry() LockEntry {
	e := LockEntry{TableSHA256: m.TableHash(), Theorems: map[string]string{}}
	for _, t := range m.Theorems {
		e.Theorems[t.Name] = t.Statement
	}
	return e
}

// Verify checks m against its lock entry: same table, the same theorems with
// the same statements — nothing added, dropped or reworded.
func (l Lock) Verify(m *Model) error {
	want, ok := l[m.ID]
	if !ok {
		return fmt.Errorf("%s: model is not in the lock; the spec owner must add it", m.ID)
	}
	got := m.Entry()
	var errs []error
	if got.TableSHA256 != want.TableSHA256 {
		errs = append(errs, fmt.Errorf("%s: oracle table changed (sha256 %s, locked %s)", m.ID, got.TableSHA256, want.TableSHA256))
	}
	for _, name := range slices.Sorted(maps.Keys(want.Theorems)) {
		stmt, ok := got.Theorems[name]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: locked theorem %s is gone", m.ID, name))
		case stmt != want.Theorems[name]:
			errs = append(errs, fmt.Errorf("%s: theorem %s reworded:\n  locked: %s\n  now:    %s", m.ID, name, want.Theorems[name], stmt))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got.Theorems)) {
		if _, ok := want.Theorems[name]; !ok {
			errs = append(errs, fmt.Errorf("%s: theorem %s is not in the lock", m.ID, name))
		}
	}
	return errors.Join(errs...)
}

// Requirement builds the requirement proven by this model: its Domain is the
// whole table and its oracle class compares every observation with the row.
// Facts must carry every input and the output. Extra classes may name regions
// worth reporting separately; they are judged as usual.
func (m *Model) Requirement(text string, extra ...evident.Class) evident.Requirement {
	index := make(map[string]any, len(m.Rows))
	rows := make([]evident.Facts, 0, len(m.Rows))
	for _, r := range m.Rows {
		index[evident.RowKey(m.Inputs, r.In)] = r.Out
		rows = append(rows, r.In)
	}
	oracle := evident.Class{
		Name:    "lean oracle",
		Source:  evident.Harness,
		When:    evident.Always,
		Require: append(slices.Clone(m.Inputs), m.Output),
		Then: func(f evident.Facts) error {
			want, ok := index[evident.RowKey(m.Inputs, f)]
			if !ok {
				return errors.New("inputs outside the model's domain")
			}
			if got := f[m.Output]; !evident.Equal(got, want) {
				return fmt.Errorf("%s = %v, model says %v", m.Output, got, want)
			}
			return nil
		},
	}
	return evident.Requirement{
		ID:      m.ID,
		Text:    text,
		Classes: append([]evident.Class{oracle}, extra...),
		Domain:  &evident.Domain{Keys: slices.Clone(m.Inputs), Rows: rows},
	}
}

// MustRequirement parses model and lock, verifies the model against the lock and
// returns its requirement; it panics on any mismatch. Meant for package-level
// spec variables, so a broken or unlocked model fails every test that uses it.
func MustRequirement(model, lock []byte, text string, extra ...evident.Class) evident.Requirement {
	m, err := Parse(model)
	if err != nil {
		panic(err)
	}
	l, err := ParseLock(lock)
	if err != nil {
		panic(err)
	}
	if err := l.Verify(m); err != nil {
		panic(err)
	}
	return m.Requirement(text, extra...)
}
