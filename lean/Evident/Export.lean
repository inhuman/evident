import Lean
import Evident.Finite

/-!
# Exporting a proven model to the Go side

`#evident_export` turns a Lean model into the JSON file `evidentlean` loads:

* `rows` — the oracle evaluated on every point of its finite domain. The Go
  harness must observe each row on the live code; the observed output must
  equal the model's.
* `theorems` — each listed theorem with its statement and the axioms its proof
  depends on. Elaboration fails if a proof uses anything beyond the standard
  axioms: `sorry` (`sorryAx`), `native_decide` (`Lean.ofReduceBool`) or a
  custom `axiom` cannot slip into an exported model. The statements are
  compared on the Go side against a lock file the spec owner keeps, so a
  weakened theorem is a visible diff.

```
#evident_export "step.run-decision" StepRun.run
  ["step_type", "skip", "only", "exclude", "destroy"] "run"
  [StepRun.excluded_never_runs, StepRun.destroy_only_narrows]
  "out/step-run-decision.json"
```

Run the file with `lake env lean <File>.lean`: it always re-elaborates, so the
JSON is rewritten even when build artefacts are cached.
-/

open Lean Elab Command Term Meta

namespace Evident

/-- Enumerates a curried function over finite arguments: one row per point of
the domain, inputs in argument order, output encoded with `ToJson`. -/
class Table (f : Type) where
  rows : f → List (List Json × Json)

instance (priority := low) [ToJson β] : Table β where
  rows b := [([], toJson b)]

instance [Finite α] [ToJson α] [Table β] : Table (α → β) where
  rows f := (Finite.all : List α).flatMap fun a =>
    (Table.rows (f a)).map fun (ins, out) => (toJson a :: ins, out)

/-- The axioms a kernel-checked proof may depend on. -/
def allowedAxioms : List Name := [``propext, ``Classical.choice, ``Quot.sound]

/-- Statement and axioms of theorem `n`; throws if it is not a theorem or its
proof leans on anything outside `allowedAxioms`. -/
def checkTheorem (n : Name) : CommandElabM (String × Array Name) := do
  let info ← getConstInfo n
  unless info matches .thmInfo _ do
    throwError "{n} is not a theorem"
  let axs ← collectAxioms n
  for a in axs do
    unless allowedAxioms.contains a do
      throwError "{n} depends on axiom {a}; only {allowedAxioms} are allowed (no sorry, no native_decide)"
  let stmt ← liftTermElabM do
    let opts := ((← getOptions).set `format.width (100000 : Nat)).set `pp.fullNames true
    withOptions (fun _ => opts) do
      return toString (← ppExpr info.type)
  return (stmt, axs)

unsafe def evalRowsUnsafe (fn : Term) : TermElabM (List (List Json × Json)) := do
  let ty ← elabType (← `(List (List Lean.Json × Lean.Json)))
  evalTerm (List (List Json × Json)) ty (← `(Evident.Table.rows $fn))

@[implemented_by evalRowsUnsafe]
opaque evalRows (fn : Term) : TermElabM (List (List Json × Json))

syntax (name := evidentExport)
  "#evident_export " str term:max "[" str,* "]" str "[" ident,* "]" str : command

@[command_elab evidentExport]
def elabExport : CommandElab
  | `(#evident_export $id:str $fn [$keys,*] $out:str [$thms,*] $path:str) => do
    let keys := keys.getElems.map (·.getString)
    let rows ← liftTermElabM <| evalRows fn
    let mut rowsJson : Array Json := #[]
    for (ins, o) in rows do
      unless ins.length == keys.size do
        throwError "model takes {ins.length} inputs but {keys.size} input names were given"
      rowsJson := rowsJson.push <| Json.mkObj
        [("in", Json.mkObj (keys.toList.zip ins)), ("out", o)]
    let mut thmsJson : Array Json := #[]
    for t in thms.getElems do
      let n ← liftCoreM <| realizeGlobalConstNoOverloadWithInfo t
      let (stmt, axs) ← checkTheorem n
      thmsJson := thmsJson.push <| Json.mkObj
        [("name", toJson n.toString), ("statement", toJson stmt),
         ("axioms", toJson (axs.map (·.toString)))]
    let doc := Json.mkObj
      [("id", toJson id.getString), ("inputs", toJson keys), ("output", toJson out.getString),
       ("rows", Json.arr rowsJson), ("theorems", Json.arr thmsJson)]
    let file : System.FilePath := path.getString
    if let some dir := file.parent then
      IO.FS.createDirAll dir
    IO.FS.writeFile file (doc.pretty ++ "\n")
    logInfo m!"{id.getString}: {rowsJson.size} rows, {thmsJson.size} kernel-checked theorems → {file}"
  | _ => throwUnsupportedSyntax

end Evident
