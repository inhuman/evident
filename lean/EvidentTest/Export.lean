import EvidentTest.Gate

#evident_export "test.gate" EvidentTest.Gate.canOpen ["role", "locked"] "open"
  [EvidentTest.Gate.locked_never_opens, EvidentTest.Gate.only_admin_opens, EvidentTest.Gate.some_role_opens]
  "../testdata/gate.json"
