// Package evidenttest wires evident verdicts into go test.
package evidenttest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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

// OutEnv names the file Main appends verdicts to as JSON lines, so a CI step can
// build one requirement × class matrix across packages.
const OutEnv = "EVIDENT_OUT"

// Main runs the package tests and then judges reqs over everything recorded in
// Suite, so classes may be covered by different tests. Call it from TestMain.
//
// A filtered run (-run/-skip) cannot cover every class; verdicts are printed but
// do not fail it. Unfiltered runs — what CI does — fail on anything but OK.
func Main(m *testing.M, reqs ...evident.Requirement) {
	code := m.Run()

	filtered := flagSet("test.run") || flagSet("test.skip")
	obs := Suite.Observations()
	var verdicts []evident.Verdict
	for _, r := range reqs {
		v := evident.Judge(r, obs)
		verdicts = append(verdicts, v)
		fmt.Fprint(os.Stderr, v.String())
		if v.Status != evident.OK && !filtered && code == 0 {
			code = 1
		}
	}

	if path := os.Getenv(OutEnv); path != "" {
		if err := appendJSONL(path, verdicts); err != nil {
			fmt.Fprintf(os.Stderr, "evident: write %s: %v\n", path, err)
			code = 1
		}
	}
	os.Exit(code)
}

func flagSet(name string) bool {
	f := flag.Lookup(name)
	return f != nil && f.Value.String() != ""
}

func appendJSONL(path string, verdicts []evident.Verdict) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, v := range verdicts {
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return nil
}
