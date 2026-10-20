import EvidentTest.Gate
theorem fast : ∀ r, EvidentTest.Gate.canOpen r true = false := by native_decide
#evident_export "test.native" EvidentTest.Gate.canOpen ["role", "locked"] "open" [fast] "/dev/null"
