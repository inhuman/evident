/-!
# Finite input spaces

A requirement's model ranges over finite types. `Finite α` lists every value of
`α` together with a proof that the list is complete, so

* quantified statements `∀ a, p a` / `∃ a, p a` over such types are decidable
  and theorems close with `decide`, checked by the kernel (no `native_decide`,
  no extra axioms);
* the exported oracle table enumerates the whole domain, not a sample.
-/

namespace Evident

class Finite (α : Type) where
  all : List α
  complete : ∀ a, a ∈ all

instance : Finite Bool where
  all := [false, true]
  complete := by intro a; cases a <;> decide

instance [Finite α] : Finite (Option α) where
  all := none :: (Finite.all : List α).map some
  complete := by
    intro a
    cases a with
    | none => exact List.mem_cons_self
    | some x => exact List.mem_cons_of_mem _ (List.mem_map_of_mem (Finite.complete x))

instance decForall [Finite α] (p : α → Prop) [DecidablePred p] : Decidable (∀ a, p a) :=
  if h : ∀ a ∈ (Finite.all : List α), p a then
    isTrue (fun a => h a (Finite.complete a))
  else
    isFalse (fun h' => h (fun a _ => h' a))

instance decExists [Finite α] (p : α → Prop) [DecidablePred p] : Decidable (∃ a, p a) :=
  if h : ∃ a ∈ (Finite.all : List α), p a then
    isTrue (let ⟨a, _, pa⟩ := h; ⟨a, pa⟩)
  else
    isFalse (fun ⟨a, pa⟩ => h ⟨a, Finite.complete a, pa⟩)

end Evident
