package main

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/inhuman/evident"
	"github.com/inhuman/evident/evidentlean"
	"github.com/inhuman/evident/evidenttest"
)

// JUnit XML as GitLab and most CI systems read it.
type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func (s *junitSuite) add(c junitCase) {
	c.ClassName = s.Name
	s.Cases = append(s.Cases, c)
	s.Tests++
	if c.Failure != nil {
		s.Failures++
	}
}

func failure(message, text string) *junitFailure {
	return &junitFailure{Message: message, Text: text}
}

// junit writes a JUnit report: a suite per judged or registered requirement
// (a case per class, the domain, unclassified observations), and with a lock a
// suite per Lean model (a case per theorem and one for the oracle table).
// Arguments: *.jsonl are EVIDENT_OUT files, *.json are model exports.
func junit(args []string, out io.Writer) error {
	var lockPath, outPath string
	var inputs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-lock", "-o":
			if i+1 == len(args) {
				return fmt.Errorf("%s needs a value", args[i])
			}
			if args[i] == "-lock" {
				lockPath = args[i+1]
			} else {
				outPath = args[i+1]
			}
			i++
		default:
			inputs = append(inputs, args[i])
		}
	}

	var suites junitSuites
	var verdictFiles, modelFiles []string
	for _, in := range inputs {
		if strings.HasSuffix(in, ".jsonl") {
			verdictFiles = append(verdictFiles, in)
		} else {
			modelFiles = append(modelFiles, in)
		}
	}
	if len(verdictFiles) == 0 {
		return errors.New("usage: evident junit [-o report.xml] [-lock lock.json] verdicts.jsonl... [model.json...]")
	}

	reqSuites, err := requirementSuites(verdictFiles)
	if err != nil {
		return err
	}
	suites.Suites = append(suites.Suites, reqSuites...)

	if len(modelFiles) > 0 {
		if lockPath == "" {
			return errors.New("model files need -lock")
		}
		modelSuites, err := leanSuites(lockPath, modelFiles)
		if err != nil {
			return err
		}
		suites.Suites = append(suites.Suites, modelSuites...)
	}

	data, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), append(data, '\n')...)
	if outPath == "" {
		_, err = out.Write(data)
		return err
	}
	return os.WriteFile(outPath, data, 0o644)
}

func requirementSuites(paths []string) ([]junitSuite, error) {
	registered := map[string]bool{}
	last := map[string]evidenttest.Line{}
	for _, path := range paths {
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
			return nil, err
		}
	}

	ids := slices.Sorted(maps.Keys(registered))
	for id := range last {
		if !registered[id] {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	suites := make([]junitSuite, 0, len(ids))
	for _, id := range ids {
		s := junitSuite{Name: id}
		l, judged := last[id]
		switch {
		case !judged:
			s.add(junitCase{Name: "judged", Failure: failure("registered but never judged",
				"no test package ran this requirement; is it missing from a TestMain?")})
		case !registered[id] && len(registered) > 0:
			s.add(junitCase{Name: "registered", Failure: failure("judged but not registered", "add it to the registry")})
			fallthrough
		default:
			addVerdictCases(&s, l)
		}
		suites = append(suites, s)
	}
	return suites, nil
}

func addVerdictCases(s *junitSuite, l evidenttest.Line) {
	v := l.Verdict
	if l.Filtered && !l.Strict {
		s.add(junitCase{Name: "enforced run", Failure: failure("judged only in a filtered, non-strict run",
			"-run/-skip were set; run with CI=true or EVIDENT_STRICT=1")})
	}

	byClass := map[string][]evident.Violation{}
	for _, vi := range v.Violations {
		byClass[vi.Class] = append(byClass[vi.Class], vi)
	}
	for _, c := range v.Classes {
		jc := junitCase{Name: c.Name, SystemOut: fmt.Sprintf("observed %d times", c.Observed)}
		switch {
		case len(byClass[c.Name]) > 0:
			jc.Failure = failure(fmt.Sprintf("%d observations violate the class", len(byClass[c.Name])), describe(byClass[c.Name]))
		case c.Observed == 0:
			jc.Failure = failure("never observed", "no test produced an observation of this class")
		}
		s.add(jc)
	}
	if vs := byClass[""]; len(vs) > 0 {
		s.add(junitCase{Name: "unclassified observations",
			Failure: failure(fmt.Sprintf("%d observations match no class", len(vs)), describe(vs))})
	}
	if d := v.Domain; d != nil {
		jc := junitCase{Name: "domain", SystemOut: fmt.Sprintf("%d/%d rows observed", d.Covered, d.Rows)}
		if d.Covered < d.Rows {
			var b strings.Builder
			for _, row := range d.Missing {
				fmt.Fprintf(&b, "missing %v\n", map[string]any(row))
			}
			jc.Failure = failure(fmt.Sprintf("%d/%d rows observed", d.Covered, d.Rows), b.String())
		}
		s.add(jc)
	}
}

// describe lists the first violations: what failed, where it was recorded.
func describe(vs []evident.Violation) string {
	const shown = 10
	var b strings.Builder
	for i, v := range vs {
		if i == shown {
			fmt.Fprintf(&b, "... %d more\n", len(vs)-shown)
			break
		}
		fmt.Fprintf(&b, "%s\n  facts: %v\n", v.Err, map[string]any(v.Facts))
		if v.Where != "" {
			fmt.Fprintf(&b, "  at %s\n", v.Where)
		}
	}
	return b.String()
}

func leanSuites(lockPath string, modelPaths []string) ([]junitSuite, error) {
	l, err := readLock(lockPath)
	if err != nil {
		return nil, err
	}
	models, err := loadModels(modelPaths)
	if err != nil {
		return nil, err
	}
	suites := make([]junitSuite, 0, len(models))
	for _, m := range models {
		s := junitSuite{Name: "lean: " + m.ID}
		want, locked := l[m.ID]
		got := m.Entry()

		table := junitCase{Name: "oracle table", SystemOut: fmt.Sprintf("%d rows, sha256 %s", len(m.Rows), got.TableSHA256)}
		switch {
		case !locked:
			table.Failure = failure("model is not in the lock", "the spec owner must lock it")
		case want.TableSHA256 != got.TableSHA256:
			table.Failure = failure("oracle table differs from the lock", "locked sha256 "+want.TableSHA256)
		}
		s.add(table)

		for _, t := range m.Theorems {
			jc := junitCase{Name: t.Name, SystemOut: fmt.Sprintf("%s\naxioms: %v", t.Statement, t.Axioms)}
			switch stmt, ok := want.Theorems[t.Name]; {
			case !locked || !ok:
				jc.Failure = failure("theorem is not in the lock", t.Statement)
			case stmt != t.Statement:
				jc.Failure = failure("theorem reworded since it was locked", "locked: "+stmt+"\nnow:    "+t.Statement)
			}
			s.add(jc)
		}
		for _, name := range slices.Sorted(maps.Keys(want.Theorems)) {
			if !slices.ContainsFunc(m.Theorems, func(t evidentlean.Theorem) bool { return t.Name == name }) {
				s.add(junitCase{Name: name, Failure: failure("locked theorem is gone", want.Theorems[name])})
			}
		}
		suites = append(suites, s)
	}
	return suites, nil
}

func readLines(path string, each func(evidenttest.Line)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		var l evidenttest.Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
		each(l)
	}
	return sc.Err()
}
