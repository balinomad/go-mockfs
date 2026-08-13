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

## `latency.go`'s serialized mode uses a channel ticket, not a mutex

Serialized (non-`Async()`) mode originally held `latencySimulator.mu` — a `sync.Mutex` — across the simulated `time.Sleep`, to make concurrent callers block behind an in-progress sleep and model blocking I/O. `sync.Mutex` operations are never durably blocking inside a `testing/synctest` bubble: a goroutine waiting on `Mutex.Lock()` isn't recognized as blocked-only-until-fake-time-advances, so a bubble with one goroutine sleeping-while-holding the lock and others waiting on it can never reach "everyone durably blocked," and the fake clock never advances. Deadlock — confirmed as an actual 60s CI hang. Not limited to this package's own tests: any consumer combining `WithLatency`/`WithPerOperationLatency` — the only mode `MockFS`/`MockFile` ever use internally; there is no public option to select `Async()` through the `MockFS`/`MockFile` API — with concurrent access to the same filesystem or file handle inside their own `synctest.Test` hits the identical deadlock.

This reverses an earlier version of this entry, which left the mutex as-is and reverted three of this package's own tests to real time instead. That fixed this repository's CI but not the defect shipped to consumers.

Fixed by removing `mu` entirely and giving its two jobs to primitives that don't have this failure mode:

- **Serialization** — a new 1-buffered `chan struct{}` (`serialize`), pre-loaded with one token, acquired before and released after the sleep in every non-`Async()` path. Channel receive on an empty channel is durably blocking under synctest, so this serializes concurrent callers exactly as the mutex did, without the deadlock. `Clone()` gives each clone its own independent ticket, matching its existing independent-`seen`-state contract.
- **`seen` tracking** — `[NumOperations]atomic.Bool` with `CompareAndSwap(false, true)`, the same pattern this package already uses for `ErrorRule.usedOnce`. A per-operation flag is a simple counter/flag, not complex shared state — exactly what `sync/atomic` is for, and it needed no mutex in the first place. `Reset()`'s per-element `Store(false)` needs no cross-element atomicity given its existing "not concurrent with `Simulate`" contract; `Clone()` was only ever reading `durations`, set once at construction and never mutated again.

## `MockFile`'s `f.mu` uses the same channel-ticket fix as `latency.go`'s `serialize`, not a narrowed critical section

All eight of `MockFile`'s locked methods (`Read`, `ReadAt`, `Write`, `WriteAt`, `Seek`, `ReadDir`, `Stat`, `Close`) held `f.mu` — a `sync.Mutex` — across the call to `LatencySimulator.Simulate()`, including its sleep. Same defect class as the entry above: a `sync.Mutex` held across a sleep is not durably blocking inside a `testing/synctest` bubble, so a bubble with one goroutine sleeping while holding `f.mu` and another blocked on `f.mu.Lock()` never reaches "all goroutines durably blocked," and the fake clock never advances. That entry's own text already named the exposure in general terms — "any consumer combining `WithLatency`/`WithPerOperationLatency` ... with concurrent access to the same filesystem or file handle inside their own `synctest.Test` hits the identical deadlock" — `f.mu` is the concrete mechanism that makes it true for file handles specifically; the `latency.go` fix alone did not close it, since it only serializes concurrent calls into a single shared `LatencySimulator`, not concurrent calls into a single `MockFile` handle.

Two fixes were evaluated:

1. **Narrow the critical section.** Release `f.mu` around `Simulate()` only, re-acquire after. Rejected. This was attempted first and reverted: releasing the lock mid-operation lets any other call on the same handle fully interleave through the gap, not just the one race (`Close` racing `Read`) initially considered — `position`, `mapFile.Data`, and `closed` are a single compound state today, and every one of the eight methods would need its own audit for what stays safe unguarded during the window. A correctness fix should not, on its own, force a decision to relax the handle's existing full-serialization guarantee.
2. **Swap `f.mu`'s primitive, keep the discipline.** Replace `sync.Mutex` with a 1-buffered `chan struct{}` (`newFileLock`), acquired once at the top of each method and held for the method's entire duration, released via `defer` — identical to how `f.mu.Lock()`/`defer f.mu.Unlock()` already worked. Adopted. Channel receive on an empty channel is durably blocking under synctest, closing the defect, while the acquire/hold/release shape is untouched: same full per-handle serialization, same atomicity of `position`/`Data`/`closed` across an entire operation, no observable change for real-time callers.

`concurrency.md`'s own default — mutexes protect shared state read/written without ownership transfer, channels transfer ownership — argues for keeping `f.mu` a real `Mutex`. The channel ticket is a deliberate, narrow exception to that default for this one class of hazard, not a general preference for channels over mutexes in this package; `newFileLock` is scoped to `mockfile.go` and does not share code with `latency.go`'s `newSerializeTicket`, despite being structurally identical, to keep the change local to the file the defect is in.

## `collectDirEntries`'s subdirectory resolution: two deterministic passes, not one order-dependent pass

The original single-pass design resolved a nested subdirectory's `FileInfo` one of two ways depending on which of its entries — the subdirectory's own map key, or one of its descendant files — Go's map iteration visited first: a dedicated subdirectory-lookup branch (`m.files[subPath]`), or the general direct-child branch reusing the current map entry, gated by a `seen`-map short-circuit whose outcome depended on iteration order. Confirmed empirically with an instrumented throwaway copy: 1,000 calls to `ReadDir` on a two-entry fixture (one subdirectory, one file nested inside it) reached the dedicated lookup 135 times and short-circuited past it the other 865, out of the same 1,000 calls — order-dependent, not a fixed split.

Both paths, when reached, produced identical output: same underlying map entry, same `Mode`/`ModTime`; `size` differed in source expression (`0` vs `len(file.Data)`) but both evaluate to `0` for a directory, and `FileInfo.Size()` independently zeroes it for `IsDir()` regardless. So no black-box test asserting on `ReadDir`'s output could ever distinguish which branch ran, or exercise either one on demand — confirmed by two earlier attempts that failed to pin this down.

Fixed by replacing the single order-dependent pass with two deterministic ones: pass one collects the set of unique immediate child names (set membership doesn't depend on iteration order); pass two does one `m.files` lookup per unique name, whichever kind of entry it turns out to be. One lookup path instead of two competing ones. Verified with the same instrumented-copy technique: the unified lookup now runs exactly 1,000/1,000 times over the same 1,000 calls, and output on a richer multi-level fixture matched the original function's output exactly. Stays O(n) — still two full passes over `m.files`, no maintained index — so it doesn't reopen "`collectDirEntries` stays O(n)" above.

One `if !exists` line in the second pass remains unreachable through the public API, for the same class of reason as `MockFile`'s invariants above: `Remove`, `RemoveAll`, `Rename`, and `RemoveEntry` — the only methods that delete or move map entries — all preserve "a directory referenced by a descendant has its own map entry" without exception. Left untested, the same treatment already given to `StatsRecorder.Record`/`Set`/`SetBytes`'s panics elsewhere in this file: not reachable through this package's own public surface. A one-line comment at the call site records why.

A new black-box test, `TestMockFS_ReadDir_NestedSubdirectory` (`mockfs_test.go`), now covers the reachable case — a scenario no prior test exercised at all, regardless of the order-dependency, since nothing previously called `ReadDir` on a directory whose listing must represent a subdirectory rather than a file. It wouldn't have failed against the old code either, since both of the old code's paths produced correct output when reached; its value is closing a coverage gap that existed independent of the flakiness, and now doing so deterministically.

## Real-time latency assertions in `mockfile_test.go` moved to `synctest.Test`

`TestMockFile_LatencyCloning` failed in CI (`-race -count=1 -shuffle=on`): `file2 first read: expected duration 50ms (±20ms), got 111.9061ms`. Traced the read path for both files before concluding this was CI scheduler contention rather than a logic defect: `f1`/`f2` are opened sequentially (no concurrency inside the test), each gets its own cloned `LatencySimulator` at `Open()` (`clonedLatency := m.latency.Clone()`, `mockfs.go`) with an independent `serialize` ticket and `seen` array, and `f.mu` is per-`MockFile` (`newFileLock()`). No blocking resource is shared between `f1.Read` and `f2.Read`; unrelated to the `f.mu` primitive swap made for the synctest deadlock fix above (`sync.Mutex` to a channel ticket), which changed the primitive, not the acquire/hold/release discipline. 111.9ms against a 50ms target with ±20ms tolerance (a constant explicitly commented "Timing tolerance for test flakiness" in `helpers_test.go`) is consistent with `time.Sleep(50ms)` losing the CPU under load from the rest of the package's `t.Parallel()` suite — several tests spin up 10-50 goroutines each (`TestMockFS_Read_Write_Concurrent`, `TestErrorInjector_Add_Concurrent`, etc.) — compounded by `-race`'s per-goroutine overhead. `-shuffle=on` exists specifically to surface load-dependent ordering effects like this.

Three fixes were evaluated:

1. **Loosen the tolerance, keep real time.** Rejected as the default: lowers failure frequency, doesn't remove the exposure, and the project already has a working alternative (see below). Kept as the fallback pattern for the one case that genuinely needs real concurrency: `TestLatencySimulator_Simulate_Async` in `latency_test.go`, which asserts an upper bound only (`tolerance*2`) because it measures actual concurrent goroutines completing together, something a fake clock can't observe the same way.
2. **No action, treat as a one-off CI blip.** Rejected: the exposure is structural (real wall-clock assertion plus `t.Parallel()` plus a growing goroutine-heavy test count), not a one-time fluke; it recurs, non-deterministically, more often as the suite grows.
3. **Move to `synctest.Test`.** Adopted. `latency_test.go` already solved this exact problem this way for its own tests — fake clock, immune to scheduler jitter, `time.Sleep` still executes and is still measured, just against virtual instead of wall-clock time. `mockfile_test.go`'s latency tests predate that migration and were never converted.

Applied to all `t.Parallel()`-marked real-time latency/duration checks in `mockfile_test.go`: `TestMockFile_LatencyCloning`, `TestMockFile_LatencyReset`, `TestMockFile_LatencyOnceMode`, `TestMockFile_LatencySharedSimulator`, `TestMockFile_WriteAt_LatencyBeforeError`, and both `TestFileOptions` subtests (`latency`, `per-operation latency`). `TestMockFile_LatencyOnceMode`'s check (`elapsed < testDuration-tolerance`) is lower-bound-only and was never actually exposed to this failure mode — only an upper-bound check can fail from overshoot — converted anyway for consistency with the rest of the file, not because it carried the same risk.

`TestMockFile_LatencySimulation` was left on real time: it does not call `t.Parallel()` (its subtests share one file and depend on sequential ordering, marked `//nolint:paralleltest`), so it does not run concurrently with the rest of the package's parallel suite and carries substantially lower exposure to this failure mode.
