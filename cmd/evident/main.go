// Command evident checks evidence-based testing artefacts in CI.
//
//	evident lock   <lock.json> <model.json>...   pin models (spec owner runs this)
//	evident verify <lock.json> <model.json>...   fail if a model drifted from its lock
//	evident gate   <verdicts.jsonl>              fail unless every registered requirement is ok
//	evident junit  [-o report.xml] [-lock lock.json] <verdicts.jsonl>... [<model.json>...]
//	                                             JUnit report: requirements, classes, domains, theorems
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/inhuman/evident"
	"github.com/inhuman/evident/evidentlean"
	"github.com/inhuman/evident/evidenttest"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "evident:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("usage: evident lock|verify <lock.json> <model.json>... | evident gate <verdicts.jsonl> | " +
			"evident junit [-o report.xml] [-lock lock.json] <verdicts.jsonl>... [<model.json>...]")
	}
	switch args[0] {
	case "lock":
		return lock(args[1], args[2:], out)
	case "verify":
		return verify(args[1], args[2:], out)
	case "gate":
		return gate(args[1], out)
	case "junit":
		return junit(args[1:], out)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func loadModels(paths []string) ([]*evidentlean.Model, error) {
	if len(paths) == 0 {
		return nil, errors.New("no model files given")
	}
	var models []*evidentlean.Model
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		m, err := evidentlean.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		models = append(models, m)
	}
	return models, nil
}

func readLock(path string) (evidentlean.Lock, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return evidentlean.Lock{}, nil
	}
	if err != nil {
		return nil, err
	}
	return evidentlean.ParseLock(data)
}

func lock(lockPath string, modelPaths []string, out io.Writer) error {
	models, err := loadModels(modelPaths)
	if err != nil {
		return err
	}
	l, err := readLock(lockPath)
	if err != nil {
		return err
	}
	for _, m := range models {
		l[m.ID] = m.Entry()
		fmt.Fprintf(out, "locked %s: %d rows, %d theorems\n", m.ID, len(m.Rows), len(m.Theorems))
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, append(data, '\n'), 0o644)
}

func verify(lockPath string, modelPaths []string, out io.Writer) error {
	models, err := loadModels(modelPaths)
	if err != nil {
		return err
	}
	l, err := readLock(lockPath)
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range models {
		if err := l.Verify(m); err != nil {
			errs = append(errs, err)
			continue
		}
		fmt.Fprintf(out, "ok %s: %d rows, %d theorems match the lock\n", m.ID, len(m.Rows), len(m.Theorems))
	}
	return errors.Join(errs...)
}

// gate reads EVIDENT_OUT lines: every registered requirement needs an ok
// verdict from an enforced run; the last verdict per requirement wins.
func gate(path string, out io.Writer) error {
	registered := map[string]bool{}
	last := map[string]evidenttest.Line{}
	if err := readLines(path, func(l evidenttest.Line) {
		switch l.Kind {
		case "registry":
			for _, id := range l.Registry {
				registered[id] = true
			}
		case "verdict":
			last[l.Verdict.Requirement] = l
		}
	}); err != nil {
		return err
	}
	if len(registered) == 0 {
		return errors.New("no registry line: call evidenttest.Registry from the spec package's tests")
	}

	var errs []error
	for _, id := range slices.Sorted(maps.Keys(registered)) {
		l, ok := last[id]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: registered but never judged", id))
		case l.Filtered && !l.Strict:
			errs = append(errs, fmt.Errorf("%s: judged only in a filtered, non-strict run", id))
		case l.Verdict.Status != evident.OK:
			errs = append(errs, fmt.Errorf("%s: %s", id, l.Verdict.Status))
		default:
			fmt.Fprintf(out, "ok %s\n", id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(last)) {
		if !registered[id] {
			errs = append(errs, fmt.Errorf("%s: judged but not registered", id))
		}
	}
	return errors.Join(errs...)
}
