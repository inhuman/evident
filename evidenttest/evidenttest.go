// Package evidenttest wires evident verdicts into go test.
package evidenttest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/inhuman/evident"
)

// Check fails t unless every requirement is OK over the ledger's observations.
// Use it when a single test is expected to cover all classes.
func Check(t testing.TB, l *evident.Ledger, reqs ...evident.Requirement) {
	t.Helper()
	obs := l.Observations()
	for _, r := range reqs {
		v := evident.Judge(r, obs)
		if v.Status != evident.OK {
			t.Errorf("requirement not proven:\n%s", v)
			continue
		}
		t.Logf("%s", v)
	}
}

// Suite is the package-wide ledger judged by Main.
var Suite = &evident.Ledger{}

// OutEnv names the file Main appends verdicts to as JSON lines, so a CI step
// (`evident gate`) can build one requirement × class matrix across packages.
const OutEnv = "EVIDENT_OUT"

// StrictEnv forces enforcement even for filtered runs. CI=true implies it, so a
// -run/-skip slipped into GOFLAGS cannot switch verdicts off in CI.
const StrictEnv = "EVIDENT_STRICT"

// Line is one JSON line of the EVIDENT_OUT file.
type Line struct {
	Kind     string           `json:"kind"` // "verdict" | "registry"
	Verdict  *evident.Verdict `json:"verdict,omitzero"`
	Registry []string         `json:"registry,omitzero"`
	Package  string           `json:"package,omitzero"`
	Commit   string           `json:"commit,omitzero"`
	Run      string           `json:"run,omitzero"`
	Filtered bool             `json:"filtered,omitzero"`
	Strict   bool             `json:"strict,omitzero"`
}

// Main runs the package tests and then judges reqs over everything recorded in
// Suite, so classes may be covered by different tests. Call it from TestMain.
func Main(m *testing.M, reqs ...evident.Requirement) {
	os.Exit(Run(m, reqs...))
}

// Run is Main for a TestMain that has teardown to do: it returns the exit code
// instead of exiting.
//
// A filtered run (-run/-skip) cannot cover every class; outside strict mode its
// verdicts are printed but do not fail it. In strict mode (CI=true or
// EVIDENT_STRICT=1) every run is enforced.
//
// Run also fails on observations whose spec is not among reqs (a typo in an ID
// would otherwise hide a violation) and on duplicate requirement IDs.
func Run(m *testing.M, reqs ...evident.Requirement) int {
	code := m.Run()

	strict := os.Getenv(StrictEnv) == "1" || os.Getenv("CI") == "true"
	filtered := flagSet("test.run") || flagSet("test.skip")
	enforce := strict || !filtered
	if filtered {
		if enforce {
			fmt.Fprintln(os.Stderr, "EVIDENT: filtered run in strict mode — verdicts are enforced")
		} else {
			fmt.Fprintln(os.Stderr, "EVIDENT: FILTERED run — verdicts are NOT enforced")
		}
	}

	fail := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "EVIDENT: "+format+"\n", args...)
		if code == 0 {
			code = 1
		}
	}

	ids := map[string]bool{}
	for _, r := range reqs {
		if ids[r.ID] {
			fail("requirement %q passed twice", r.ID)
		}
		ids[r.ID] = true
	}

	obs := Suite.Observations()
	for _, o := range obs {
		if !ids[o.Spec] {
			fail("observation for unknown requirement %q (facts %v, at %s)", o.Spec, map[string]any(o.Facts), o.Where)
		}
	}

	var lines []Line
	for _, r := range reqs {
		v := evident.Judge(r, obs)
		fmt.Fprint(os.Stderr, v.String())
		if v.Status != evident.OK && enforce {
			fail("requirement %q is %s", r.ID, v.Status)
		}
		lines = append(lines, meta(Line{Kind: "verdict", Verdict: &v, Filtered: filtered, Strict: strict}))
	}

	if err := appendLines(lines); err != nil {
		fail("write %s: %v", os.Getenv(OutEnv), err)
	}
	return code
}

// Registry records the full list of requirements of a project in EVIDENT_OUT,
// so `evident gate` can fail on a requirement nobody judged. Call it from one
// test in the package that owns the specs; it also fails on duplicate IDs.
func Registry(t testing.TB, reqs ...evident.Requirement) {
	t.Helper()
	ids := make([]string, 0, len(reqs))
	for _, r := range reqs {
		if slices.Contains(ids, r.ID) {
			t.Errorf("requirement %q registered twice", r.ID)
		}
		ids = append(ids, r.ID)
	}
	if err := appendLines([]Line{meta(Line{Kind: "registry", Registry: ids})}); err != nil {
		t.Errorf("write %s: %v", os.Getenv(OutEnv), err)
	}
}

func meta(l Line) Line {
	l.Commit = firstEnv("EVIDENT_COMMIT", "CI_COMMIT_SHA")
	l.Run = firstEnv("EVIDENT_RUN", "CI_PIPELINE_ID")
	if len(os.Args) > 0 {
		l.Package = os.Args[0]
	}
	return l
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func flagSet(name string) bool {
	f := flag.Lookup(name)
	return f != nil && f.Value.String() != ""
}

func appendLines(lines []Line) error {
	path := os.Getenv(OutEnv)
	if path == "" || len(lines) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			return err
		}
	}
	return nil
}
