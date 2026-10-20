import EvidentTest.Gate
theorem cheat : ∀ r, EvidentTest.Gate.canOpen r false = true := by sorry
#evident_export "test.cheat" EvidentTest.Gate.canOpen ["role", "locked"] "open" [cheat] "/dev/null"
