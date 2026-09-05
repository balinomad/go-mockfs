# Design Decisions

Records design and implementation choices that were evaluated and rejected, so they are not re-proposed without this context. Entries reflect the current state of the package; if a decision is later reversed, update the entry rather than leaving it stale.

## `Sub()` clones the latency simulator instead of sharing it

`Sub()` clones the parent's latency simulator. This gives each sub-filesystem independent `Once()` state. Sharing live latency state with the parent was evaluated and rejected: cloned/independent behavior matches how `Open()` already gives each file handle its own independent state, and is safer for concurrent tests.

## Constructors return `(T, error)`; `Must*` wrappers replace direct panicking

`NewMockFS`, `NewMockFile`, `NewLatencySimulator`, `NewLatencySimulatorPerOp`, and `NewFileInfo` panicked unconditionally on invalid input through rc.2. Three alternatives were evaluated for rc.3:

1. **Keep panicking, document it as an intentional `Must`-style exception.** Rejected: contradicts this project's own `go-standards` skill ("No panic in library code"), and unlike `regexp.MustCompile`/`template.Must`, there was no non-panicking sibling to opt out into — the panic wasn't actually optional.
2. **Full `(T, error)` return, with a `Must*` panicking counterpart for each.** Adopted. Matches the `regexp.Compile`/`MustCompile` shape properly (both forms exist), extends the error-return pattern `NewGlobMatcher`/`NewRegexpMatcher`/`AddExact`/`NewErrorRule` already used for config validation, and is skill-compliant.
3. **`TestReporter` injection** (`NewMockFS(t TestReporter, opts...)`, calling `t.Fatal` on misuse — matching `gomock`/`testify`). Rejected for the constructor tier: it would force every constructor to require a `*testing.T`-like value in scope, which `mockfs` otherwise doesn't need (package-level fixtures, benchmarks, non-test helper packages). Retained as-is for `Stats.Expect().Assert(t)` specifically, where a reporter is already the operation's whole purpose — not treated as precedent for the constructors.

`StatsRecorder.Record`/`Set`/`SetBytes` are the one deliberate exception, left panicking. Every reachable misuse of these three requires a custom filesystem, built against the exported `StatsRecorder` interface, called with invalid data directly — not reachable through `mockfs`'s own constructors or options. They are also called from `defer` at every internal record site, where an error-returning signature would break the pattern, for a narrow, advanced-only misuse surface.

`ErrorRule.Mode`'s post-construction mutability was flagged here during this work, then closed — see "`ErrorRule.Mode` is a method, `AfterN`/`Err` stay plain fields" below.

## `ErrorInjector` stays a single 13-method interface

Splitting `ErrorInjector` into something smaller was evaluated and rejected: it would force glob/regexp pattern recompilation on every call instead of once at construction time.

## `collectDirEntries` stays O(n)

`collectDirEntries` scans the entire file map on every `ReadDir` call (O(n) in total file count). A maintained parent-to-children index would make this O(k) in the directory's own children. It would require correctly updating the index across ten mutation paths: `AddFile`, `AddDir`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `WriteFile`'s create path, `RemoveEntry`, `copyFilesToSubFS`. An index that drifts from `m.files` would be a worse bug than the scan it replaces. The scan stays O(n): acceptable for the fixture sizes a test mock is expected to hold.

## Go version floor raised to 1.25 (not pinned via `toolchain`)

The floor was `1.22` through rc.3 drafting. The floor was raised to `1.25` after [GO-2025-3750](https://pkg.go.dev/vuln/GO-2025-3750) (CVE-2025-0913) turned up no fix on the `1.22`, `1.23`, or `1.24` branches. All three were already end-of-life by the time the CVE was published, so no patched release exists or ever will, for any of them.

Two ways to get CI onto a patched toolchain were evaluated:

1. **Keep `go 1.22`, add a `toolchain go1.26.5` line.** Rejected: a `toolchain` line pins an exact patch and does not move on its own. It goes stale the same way the bare `go 1.22` line already did, just reset to a later starting point. `go.mod` would also claim a floor that isn't actually buildable or passable in CI without the override. This misstates what the module requires.
2. **Raise the `go` directive itself, no `toolchain` line.** Adopted. `go-version-file: "go.mod"` (used by every CI job except the `test` matrix's `stable` leg) resolves to the latest patch of whatever major is declared, automatically, on every run. This is self-updating for as long as that major stays supported, not a one-time fix.

`1.25` is chosen over `1.26`. It is the older of the two currently-supported majors. It excludes the fewest consumers and stays fully inside the patch window.

## Benchmarks use `b.Loop()`

Originally deferred: `b.Loop()` requires Go 1.24. The module's floor was `1.22`. Superseded: the floor moved to `1.25` (see "Go version floor raised to 1.25" above). This cleared the blocker. All benchmarks now use `for b.Loop()` in place of `for i := 0; i < b.N; i++`. `b.ResetTimer()` stays where it already was; dropping it isn't part of the `b.Loop()` migration, it's separate, unneeded cleanup. `BenchmarkSimulate_Parallel` (`latency_test.go`) is unaffected: it uses `b.RunParallel`/`pb.Next()`, which has no `b.Loop()` form.

## `ErrorInjector.GetAll()` stays a map, not `iter.Seq2`

Evaluated converting to `iter.Seq2[Operation, []*ErrorRule]` alongside `Stats.FailedOperations()`'s move to `iter.Seq[Operation]`. Rejected: `GetAll()` already returns a map and already ranges with `for k, v := range` either way. Its one real consumer (`TestErrorInjector_GetAll`) does indexed, repeated lookup by `Operation` — exactly what a map gives for free, and what an iterator would tax via a mandatory `maps.Collect` first. `FailedOperations` converted because it exposed a slice for a walk-once pattern. `GetAll` isn't that shape.

## `ErrorRule.Mode` is a method, `AfterN`/`Err` stay plain fields

`NewErrorRule` validates `mode` at construction, but as a plain exported field, `Mode` could still be overwritten afterward. `rule.Mode = ErrorMode(999)` bypassed the validation entirely and surfaced as a panic later, inside `CheckAndApply`, far from the actual mistake. Unexported to `mode` with a `Mode() ErrorMode` getter. This closes the gap at the type level, rather than relying on callers not to do it.

`AfterN` and `Err` were evaluated for the same treatment and rejected. `AfterN` has no invalid value to guard against: it's a plain `uint64` used only as a numeric comparison threshold in `shouldReturnError()`, and no value of it can cause a panic. `Err` set to `nil` post-construction makes a rule silently inert, rather than panicking. A matched rule with a nil `Err` returns `nil` from `CheckAndApply`, indistinguishable from "didn't match." This is a real footgun, but much narrower and self-inflicted than `Mode`'s. Unexporting `Err` would also cost the legitimate read access `GetAll()` consumers already rely on (`errors.Is(rule.Err, ...)`), for comparatively little gain.

## `latency.go`'s serialized mode uses a channel ticket, not a mutex

Serialized (non-`Async()`) mode originally held `latencySimulator.mu` — a `sync.Mutex` — across the simulated `time.Sleep`, to make concurrent callers block behind an in-progress sleep. `sync.Mutex` waits are never durably blocking inside a `testing/synctest` bubble: a goroutine sleeping while holding the lock, with others waiting on it, means the bubble never reaches "everyone durably blocked," so the fake clock never advances. Deadlock — confirmed as an actual 60s CI hang, and not limited to this package's own tests: any consumer combining `WithLatency`/`WithPerOperationLatency` — the only mode `MockFS`/`MockFile` use internally — with concurrent access inside their own `synctest.Test` hits the same deadlock.

An earlier fix reverted three of this package's own tests to real time instead of touching the mutex. That fixed this repository's CI, not the defect shipped to consumers.

The fix replaces `mu` with two primitives that do not share its failure mode:

- **Serialization**: a 1-buffered `chan struct{}` (`serialize`), acquired before and released after the sleep. A channel receive on an empty channel is durably blocking under synctest, so this serializes callers the same way the mutex did, without the deadlock. `Clone()` gives each clone its own ticket.
- **`seen` tracking**: `[NumOperations]atomic.Bool` with `CompareAndSwap`, the same pattern already used for `ErrorRule.usedOnce`. This never needed a mutex — a per-operation flag is simple shared state, exactly what `sync/atomic` is for.

## `MockFile`'s `f.mu` uses the same channel-ticket fix as `latency.go`'s `serialize`, not a narrowed critical section

All eight of `MockFile`'s locked methods (`Read`, `ReadAt`, `Write`, `WriteAt`, `Seek`, `ReadDir`, `Stat`, `Close`) held `f.mu` — a `sync.Mutex` — across the call to `LatencySimulator.Simulate()`, including its sleep. Same defect class as "`latency.go`'s serialized mode uses a channel ticket, not a mutex" above: the mutex is not durably blocking inside a `testing/synctest` bubble. That entry's own text already named the exposure in general terms for "concurrent access to the same filesystem or file handle"; `f.mu` is the concrete mechanism that makes it true for file handles specifically. The `latency.go` fix alone did not close it, since it only serializes calls into a shared `LatencySimulator`, not calls into a single `MockFile` handle.

Two fixes were evaluated:

1. **Narrow the critical section.** Release `f.mu` around `Simulate()` only, re-acquire after. Rejected. This was attempted first and reverted: releasing the lock mid-operation lets any other call on the same handle fully interleave through the gap, not just the one race (`Close` racing `Read`) initially considered. `position`, `mapFile.Data`, and `closed` are a single compound state today, and every one of the eight methods would need its own audit for what stays safe unguarded during the window. A correctness fix should not, on its own, force a decision to relax the handle's existing full-serialization guarantee.
2. **Swap `f.mu`'s primitive, keep the discipline.** Replace `sync.Mutex` with a 1-buffered `chan struct{}` (`newFileLock`), acquired once at the top of each method and held for the method's entire duration, released via `defer` — identical to how `f.mu.Lock()`/`defer f.mu.Unlock()` already worked. Adopted. A channel receive on an empty channel is durably blocking under synctest, closing the defect, while the acquire/hold/release shape is untouched: same full per-handle serialization, same atomicity of `position`/`Data`/`closed` across an entire operation, no observable change for real-time callers.

This is a deliberate, narrow exception to `concurrency.md`'s general default (mutexes for shared state, channels for ownership transfer), not a preference for channels generally in this package. `newFileLock` stays local to `mockfile.go`, separate from `latency.go`'s equivalent ticket, to keep the fix scoped to the file with the defect.

## `collectDirEntries`'s subdirectory resolution: two deterministic passes, not one order-dependent pass

The original single-pass design resolved a nested subdirectory's `FileInfo` one of two ways depending on which of its map entries — the subdirectory's own key, or a descendant file — Go's map iteration visited first: a dedicated subdirectory-lookup branch, or the general direct-child branch, gated by a `seen`-map short-circuit whose outcome depended on iteration order. Confirmed empirically as non-deterministic with an instrumented copy.

Both paths produced identical output whenever reached: same map entry, same `Mode`/`ModTime`; the differing `size` expression both evaluate to `0` for a directory regardless. So no black-box test could ever distinguish which branch ran, and coverage of the subdirectory-lookup path was flaky across runs, not a correctness bug.

Fixed by replacing the order-dependent pass with two deterministic ones: pass one collects the set of unique immediate child names (order-independent, since it only checks set membership); pass two does one `m.files` lookup per unique name. One lookup path instead of two competing ones. Confirmed deterministic with the same instrumentation, and output matched the original function's on a richer fixture. Stays O(n) — still two full passes, no maintained index — consistent with "`collectDirEntries` stays O(n)" above.

One `if !exists` line in the second pass is unreachable through the public API: `Remove`, `RemoveAll`, `Rename`, and `RemoveEntry` — the only methods that delete or move map entries — all preserve the invariant that a directory referenced by a descendant has its own map entry. Left untested, the same treatment given to `StatsRecorder`'s panics elsewhere in this file.

A new black-box test, `TestMockFS_ReadDir_NestedSubdirectory` (`mockfs_test.go`), now covers this case — no prior test exercised a directory listing that must represent a subdirectory rather than a file.

## Real-time latency assertions in `mockfile_test.go` moved to `synctest.Test`

`TestMockFile_LatencyCloning` failed in CI (`-race -count=1 -shuffle=on`): `file2 first read: expected duration 50ms (±20ms), got 111.9061ms`. Investigation ruled out a logic defect: `f1`/`f2` share no blocking resource, and this is unrelated to the `f.mu` primitive swap in the entry above, which changed the mutex, not the serialization discipline. 111.9ms against a 50ms target (±20ms tolerance, explicitly commented "Timing tolerance for test flakiness" in `helpers_test.go`) is consistent with `time.Sleep(50ms)` losing the CPU under load from the rest of the package's `t.Parallel()` suite — several tests spin up 10-50 goroutines each — compounded by `-race` overhead. `-shuffle=on` exists specifically to surface load-dependent effects like this.

Three fixes were evaluated:

1. **Loosen the tolerance, keep real time.** Rejected as the default: lowers failure frequency but doesn't remove the exposure. Kept as the fallback for the one case that genuinely needs real concurrency: `TestLatencySimulator_Simulate_Async` in `latency_test.go`, which asserts an upper bound only (`tolerance*2`) because it measures concurrent goroutines completing together — something a fake clock can't observe the same way.
2. **No action, treat as a one-off CI blip.** Rejected: the exposure is structural — real wall-clock assertion plus `t.Parallel()` plus a growing goroutine-heavy test count — not a one-time fluke.
3. **Move to `synctest.Test`.** Adopted. `latency_test.go` already solved this exact problem this way — fake clock, immune to scheduler jitter, `time.Sleep` still executes and is still measured, just against virtual instead of wall-clock time. `mockfile_test.go`'s latency tests predate that migration.

Applied to all `t.Parallel()`-marked real-time latency checks in `mockfile_test.go`: `TestMockFile_LatencyCloning`, `TestMockFile_LatencyReset`, `TestMockFile_LatencyOnceMode`, `TestMockFile_LatencySharedSimulator`, `TestMockFile_WriteAt_LatencyBeforeError`, and both `TestFileOptions` subtests (`latency`, `per-operation latency`). `TestMockFile_LatencyOnceMode`'s check is lower-bound-only and was never actually exposed to this failure mode — converted anyway for consistency, not because it carried the same risk.

`TestMockFile_LatencySimulation` was left on real time: it doesn't call `t.Parallel()` — its subtests share one file and depend on sequential ordering (`//nolint:paralleltest`) — so it doesn't run concurrently with the rest of the package's parallel suite.

## `Add*ForAllOps` methods validate `mode` through a shared `validateModeAndAfter`, not inline checks

`AddExactForAllOps`, `AddGlobForAllOps`, `AddRegexpForAllOps`, and `AddAllForAllOps` called `validateAfter(after, mode)` directly, bypassing the `mode.IsValid()` check `NewErrorRule` performs before calling that same function. `AddExact`, `AddGlob`, `AddRegexp`, and `AddAll` were unaffected only because they happen to route through `NewErrorRule` — not from any explicit decision to validate mode for four of the eight methods and not the other four. An invalid `ErrorMode` passed to any of the four `*ForAllOps` methods fell through `validateAfter`'s `default: return 0, nil` branch, was accepted silently, and only surfaced later as a panic inside `CheckAndApply` → `shouldReturnError()`'s `default` case — the third time a mode-validation gap in this file has reached that same panic, after `NewErrorRule`'s original missing check (closed pre-rc.3) and `ErrorRule.Mode`'s post-construction mutability (see "`ErrorRule.Mode` is a method, `AfterN`/`Err` stay plain fields" above).

Two fixes were evaluated:

1. **Add `if !mode.IsValid() { return ... }` inline to each of the four functions.** Rejected: same shape of omission that caused the bug — the check would live at four separate call sites instead of one, so a ninth function added later to this file could repeat the mistake exactly as these four did relative to `NewErrorRule`.
2. **Extract a shared `validateModeAndAfter(mode ErrorMode, after int) (uint64, error)`, used by `NewErrorRule` and all four `*ForAllOps` methods.** Adopted. Makes "mode invalid" and "after negative for a mode that reads it" one validation contract enforced in one place; nothing can get one check without the other. `validateAfter` is unchanged and now has a single caller (`validateModeAndAfter`).

No regression: checked every call site of these eight methods across `error_test.go`, `mockfs_test.go`, and `mockfile_test.go` — none passes an invalid mode or negative `after`, so no previously-passing call starts failing.
