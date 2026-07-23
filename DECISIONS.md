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

## Benchmarks do not use `b.Loop()`

Go 1.24 introduced `b.Loop()` for benchmarks. Not adopted: the module's Go version floor is `1.22`, and benchmark style should not require a newer Go version than the module itself supports. Revisit if the floor moves to 1.24.
