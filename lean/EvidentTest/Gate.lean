import Evident

/-! A tiny model used by the library's own tests: an access gate. -/

namespace EvidentTest.Gate
open Evident

inductive Role where
  | guest | user | admin
  deriving DecidableEq, Repr, Lean.ToJson

instance : Finite Role where
  all := [.guest, .user, .admin]
  complete := by intro r; cases r <;> decide

/-- May `role` open the admin page, given whether the account is `locked`? -/
def canOpen : Role → Bool → Bool
  | .admin, false => true
  | _, _ => false

theorem locked_never_opens : ∀ r, canOpen r true = false := by decide

theorem only_admin_opens : ∀ r l, canOpen r l = true → r = .admin := by decide

theorem some_role_opens : ∃ r, canOpen r false = true := by decide

end EvidentTest.Gate
