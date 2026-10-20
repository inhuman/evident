# evident

Evidence-based testing for Go: a requirement is proven only when **every class of
its input space was observed** and every observation satisfied its predicate.

Tests that exercise one easy path get `incomplete`, not green. Facts measured by the
test harness can be required where self-report by the code under test is not enough.

```go
var CORSAllowlist = evident.Requirement{
	ID: "cors.allowlist",
	Classes: []evident.Class{
		{Name: "allowed", Source: evident.Harness, When: evident.Is("origin", "listed"), Then: evident.Expect("header_set", true)},
		{Name: "denied", Source: evident.Harness, When: evident.Is("origin", "foreign"), Then: evident.Expect("header_set", false)},
		{Name: "absent", Source: evident.Harness, When: evident.Is("origin", "none"), Then: evident.Expect("header_set", false)},
	},
}
```

Code under test logs facts at decision points — an ordinary structured log line in production:

```go
evidentzap.Observe(logger, "dns.forward", zap.String("route", "corp"), zap.Int("rcode", rcode))
```

Tests capture and judge:

```go
var ledger evident.Ledger
logger := evidentzap.Capture(zap.NewNop(), &ledger)
// ... exercise the code, record harness facts:
ledger.Witness("cors.allowlist", evident.Facts{"origin": "foreign", "header_set": resp.Header.Get("Access-Control-Allow-Origin") != ""})
evidenttest.Check(t, &ledger, CORSAllowlist)
```

Or judge a whole package, so classes may be covered by different tests:

```go
func TestMain(m *testing.M) { evidenttest.Main(m, CORSAllowlist, DNSForward) }
```

`EVIDENT_OUT=verdicts.jsonl go test ./...` appends every verdict as a JSON line —
one requirement × class matrix for the whole repository.

Verdicts:

| status | meaning |
|---|---|
| `ok` | every class observed, every predicate held |
| `incomplete` | some class never observed |
| `violated` | a predicate failed, or an observation fell outside every class |
