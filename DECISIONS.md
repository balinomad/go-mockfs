# Design Decisions

Records design and implementation choices that were evaluated and rejected, so they are not re-proposed without this context. Entries reflect the current state of the package; if a decision is later reversed, update the entry rather than leaving it stale.

## `Sub()` clones the latency simulator instead of sharing it

`Sub()` clones the parent's latency simulator, giving each sub-filesystem independent `Once()` state. Sharing live latency state with the parent was evaluated and rejected: cloned/independent behavior matches how `Open()` already gives each file handle its own independent state, and is safer for concurrent tests.

## Constructors return `(T, error)`; `Must*` wrappers replace direct panicking

`NewMockFS`, `NewMockFile`, `NewLatencySimulator`, `NewLatencySimulatorPerOp`, and `NewFileInfo` panicked unconditionally on invalid input through rc.2. Three alternatives were evaluated for rc.3:

1. **Keep panicking, document it as an intentional `Must`-style exception.** Rejected: contradicts this project's own `go-standards` skill ("No panic in library code"), and unlike `regexp.MustCompile`/`template.Must`, there was no non-panicking sibling to opt out into — the panic wasn't actually optional.
2. **Full `(T, error)` return, with a `Must*` panicking counterpart for each.** Adopted. Matches the `regexp.Compile`/`MustCompile` shape properly (both forms exist), extends the error-return pattern `NewGlobMatcher`/`NewRegexpMatcher`/`AddExact`/`NewErrorRule` already used for config validation, and is skill-compliant.
3. **`TestReporter` injection** (`NewMockFS(t TestReporter, opts...)`, calling `t.Fatal` on misuse — matching `gomock`/`testify`). Rejected for the constructor tier: it would force every constructor to require a `*testing.T`-like value in scope, which `mockfs` otherwise doesn't need (package-level fixtures, benchmarks, non-test helper packages). Retained as-is for `Stats.Expect().Assert(t)` specifically, where a reporter is already the operation's whole purpose — not treated as precedent for the constructors.

`StatsRecorder.Record`/`Set`/`SetBytes` are the one deliberate exception, left panicking: every reachable misuse of these three requires implementing a custom filesystem against the exported `StatsRecorder` interface and calling them with invalid data directly — not reachable through `mockfs`'s own constructors or options — and they're called from `defer` at every internal record site, where an `error`-returning signature would break the pattern for a narrow, advanced-only misuse surface.

`ErrorRule.Mode` is exported and unvalidated after construction: `NewErrorRule` now rejects an invalid `ErrorMode` up front, but a caller can still set `rule.Mode = ErrorMode(999)` post-construction and hit the pre-existing panic in `shouldReturnError()` at `CheckAndApply` time. Left open — closing it needs either an unexported field (API change) or a defensive re-check in `shouldReturnError()`; not addressed in rc.3.

## `ErrorInjector` stays a single 13-method interface

Splitting `ErrorInjector` into something smaller was evaluated and rejected: it would force glob/regexp pattern recompilation on every call instead of once at construction time.

## `collectDirEntries` stays O(n)

`collectDirEntries` scans the entire file map on every `ReadDir` call (O(n) in total file count). A maintained parent-to-children index would make this O(k) in the directory's own children, but requires correctly updating the index across ten mutation paths (`AddFile`, `AddDir`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `WriteFile`'s create path, `RemoveEntry`, `copyFilesToSubFS`). An index that drifts from `m.files` would be a worse bug than the scan it replaces. Left as O(n): acceptable for the fixture sizes a test mock is expected to hold.

## Go version floor raised to 1.25 (not pinned via `toolchain`)

The floor was `1.22` through rc.3 drafting. Raised to `1.25` after [GO-2025-3750](https://pkg.go.dev/vuln/GO-2025-3750) (CVE-2025-0913) turned up no fix on the 1.22, 1.23, or 1.24 branches — all three were already end-of-life by the time the CVE was published, so no patched release exists or ever will for any of them.

Two ways to get CI onto a patched toolchain were evaluated:

1. **Keep `go 1.22`, add a `toolchain go1.26.5` line.** Rejected: a `toolchain` line pins an exact patch and does not move on its own — it goes stale the same way the bare `go 1.22` line already had, just reset to a later starting point. It also leaves `go.mod` claiming a floor that isn't actually buildable/passable in CI without the override, which misstates what the module requires.
2. **Raise the `go` directive itself, no `toolchain` line.** Adopted. `go-version-file: "go.mod"` (used by every CI job except the `test` matrix's `stable` leg) then resolves to the latest patch of whatever major is declared, automatically, on every run — self-updating for as long as that major stays supported, not a one-time fix.

`1.25` chosen over `1.26`: older of the two currently-supported majors, excludes the fewest consumers while staying fully inside the patch window.

## Benchmarks use `b.Loop()`

Originally deferred: `b.Loop()` requires Go 1.24, and the module's floor was `1.22`. Superseded — the floor moved to `1.25` (see "Go version floor raised to 1.25" above), clearing the blocker. All benchmarks now use `for b.Loop()` in place of `for b.Loop()`; `b.ResetTimer()` stays where it already was — dropping it isn't part of the `b.Loop()` migration, it's a separate, unneeded cleanup. `BenchmarkSimulate_Parallel` (`latency_test.go`) is unaffected: it uses `b.RunParallel`/`pb.Next()`, which has no `b.Loop()` form.

## `ErrorInjector.GetAll()` stays a map, not `iter.Seq2`

Evaluated converting to `iter.Seq2[Operation, []*ErrorRule]` alongside `Stats.FailedOperations()`'s move to `iter.Seq[Operation]`. Rejected: `GetAll()` already returns a map, already ranges with `for k, v := range` either way, and its one real consumer (`TestErrorInjector_GetAll`) does indexed, repeated lookup by `Operation` — exactly what a map gives for free and an iterator would tax via a mandatory `maps.Collect` first. `FailedOperations` converted because it was exposing a slice for a walk-once pattern; `GetAll` isn't that shape.

## `ErrorRule.Mode` is a method, `AfterN`/`Err` stay plain fields

`NewErrorRule` validates `mode` at construction, but as a plain exported field, `Mode` could still be overwritten afterward — `rule.Mode = ErrorMode(999)` bypassed the validation entirely and surfaced as a panic later, inside `CheckAndApply`, far from the actual mistake. Unexported to `mode` with a `Mode() ErrorMode` getter, closing the gap at the type level rather than relying on callers not to do it.

`AfterN` and `Err` were evaluated for the same treatment and rejected. `AfterN` has no invalid value to guard against — it's a plain `uint64` used only as a numeric comparison threshold in `shouldReturnError()`; no value of it can cause a panic. `Err` set to `nil` post-construction makes a rule silently inert (a matched rule with a nil `Err` returns `nil` from `CheckAndApply`, indistinguishable from "didn't match") rather than panicking — a real but much narrower, self-inflicted footgun — and unexporting it would cost the legitimate read access `GetAll()` consumers already rely on (`errors.Is(rule.Err, ...)`) for comparatively little gain.
