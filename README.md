# evident

Evidence-based testing for Go, with specifications proven in Lean.

A requirement is proven only when **every class of its input space was observed on
the live code** and every observation satisfied its predicate. For a finite domain the
specification can be a **Lean model**: its properties are kernel-checked theorems, its
oracle is enumerated over the whole domain, and the Go side must observe every row.

The aim is to make a green run hard to fake — by people or by coding agents:

| Shortcut | What stops it |
|---|---|
| test one easy path | other classes / domain rows stay unobserved → `incomplete` |
| weaken an assertion | predicates (`Then`) and oracles live in the spec, not in the test |
| self-report instead of measuring | `Source: Harness` classes ignore implementation facts; harness facts are stamped with `file:line` and rejected unless recorded in a `_test.go` file |
| omit a fact the predicate reads | `Class.Require` → missing key is a violation |
| input the spec never mentions | unclassified / outside the domain → `violated` |
| typo in a requirement ID | observation for an unknown requirement fails the package |
| `-run`/`-skip` (or `GOFLAGS`) to switch enforcement off | strict mode (`CI=true` or `EVIDENT_STRICT=1`) enforces filtered runs |
| a requirement nobody judges | `evidenttest.Registry` + `evident gate` fail on registered-but-unjudged IDs |
| `sorry`, `native_decide`, custom `axiom` in a proof | `#evident_export` fails elaboration; `evidentlean.Parse` re-checks axioms |
| weaken / drop a theorem, hand-edit the table | `evident verify` against the lock file the spec owner keeps |

What it cannot do on its own: stop someone who owns both the spec and the code.
Keep the spec package, the lock file and the Lean model under a separate owner (CODEOWNERS).

## Go: requirements, facts, verdicts

```go
var CORSAllowlist = evident.Requirement{
	ID: "cors.allowlist",
	Classes: []evident.Class{
		{Name: "allowed", Source: evident.Harness, When: evident.Is("origin", "listed"),
			Then: evident.Expect("header_set", true), Require: []string{"header_set"}},
		{Name: "denied", Source: evident.Harness, When: evident.Is("origin", "foreign"),
			Then: evident.Expect("header_set", false), Require: []string{"header_set"}},
	},
}
```

Code under test logs facts at decision points invisible from outside — an ordinary
structured log line in production:

```go
evidentzap.Observe(logger, "dns.forward", zap.String("route", "corp"), zap.Int("rcode", rcode))
```

Tests measure facts and judge the whole package:

```go
func TestMain(m *testing.M) {
	evidentzap.Attach(evidenttest.Suite) // impl observations, immune to log levels/samplers
	os.Exit(evidenttest.Run(m, spec.CORSAllowlist))
}

func TestCORS(t *testing.T) {
	resp := do(router, "https://evil.example")
	evidenttest.Suite.Witness("cors.allowlist", evident.Facts{
		"origin": "foreign", "header_set": resp.Header.Get("Access-Control-Allow-Origin") != "",
	})
}
```

| status | meaning |
|---|---|
| `ok` | every class (and every domain row) observed, every predicate held |
| `incomplete` | some class or domain row never observed |
| `violated` | a predicate failed, a required fact is missing, an observation is unclassified or outside the domain, or a harness fact came from non-test code |

## Lean: proven models

`lean/` is a Lake package:

```toml
[[require]]
name = "evident"
git = "https://github.com/inhuman/evident"
rev = "v0.2.0"
subDir = "lean"
```

```lean
import Evident
open Evident

inductive Role where | guest | user | admin
  deriving DecidableEq, Repr, Lean.ToJson

instance : Finite Role where
  all := [.guest, .user, .admin]
  complete := by intro r; cases r <;> decide

def canOpen : Role → Bool → Bool
  | .admin, false => true
  | _, _ => false

theorem locked_never_opens : ∀ r, canOpen r true = false := by decide

#evident_export "test.gate" canOpen ["role", "locked"] "open"
  [locked_never_opens] "spec/gate.json"
```

`Finite` makes `∀`/`∃` over the domain decidable, so `decide` closes theorems in the
kernel. `#evident_export` enumerates the oracle over the whole domain, records each
theorem's statement and axioms, and refuses anything beyond `propext`,
`Classical.choice`, `Quot.sound`. Run it with `lake env lean Export.lean`.

On the Go side:

```go
//go:embed gate.json
var gateModel []byte
//go:embed evident.lock.json
var lock []byte

var Gate = evidentlean.MustRequirement(gateModel, lock, "only an unlocked admin opens")
```

The requirement's `Domain` is the whole table; its oracle class compares every
observation with the model. The harness must drive the live code through **every row**.

## CLI

```
go run github.com/inhuman/evident/cmd/evident lock   evident.lock.json spec/*.json   # spec owner
go run github.com/inhuman/evident/cmd/evident verify evident.lock.json spec/*.json   # CI
EVIDENT_OUT=verdicts.jsonl CI=true go test ./...
go run github.com/inhuman/evident/cmd/evident gate   verdicts.jsonl                  # CI
```

In CI also regenerate the Lean exports and `git diff --exit-code` them, so the JSON
always matches the proven model.
