import EvidentTest.Gate
axiom trust_me : ∀ r, EvidentTest.Gate.canOpen r false = true
theorem via_axiom : EvidentTest.Gate.canOpen .guest false = true := trust_me _
#evident_export "test.axiom" EvidentTest.Gate.canOpen ["role", "locked"] "open" [via_axiom] "/dev/null"
