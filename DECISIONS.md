# Design Decisions

Records design and implementation choices that were evaluated and rejected, so they are not re-proposed without this context. Entries reflect the current state of the package; if a decision is later reversed, update the entry rather than leaving it stale.

## `Sub()` clones the latency simulator instead of sharing it

`Sub()` clones the parent's latency simulator, giving each sub-filesystem independent `Once()` state. Sharing live latency state with the parent was evaluated and rejected: cloned/independent behavior matches how `Open()` already gives each file handle its own independent state, and is safer for concurrent tests.

## Exported constructors panic on invalid input

`NewMockFS`, `NewLatencySimulator`, `NewLatencySimulatorPerOp`, `NewMockFile`, `NewFileInfo`, and `StatsRecorder.Record`/`Set`/`SetBytes` panic on invalid input by design — the same convention as `regexp.MustCompile`/`template.Must`. See `doc.go`'s "Panic Policy" for the full rationale and trigger list.

## `ErrorInjector` stays a single 13-method interface

Splitting `ErrorInjector` into something smaller was evaluated and rejected: it would force glob/regexp pattern recompilation on every call instead of once at construction time.

## `collectDirEntries` stays O(n)

`collectDirEntries` scans the entire file map on every `ReadDir` call (O(n) in total file count). A maintained parent-to-children index would make this O(k) in the directory's own children, but requires correctly updating the index across ten mutation paths (`AddFile`, `AddDir`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `WriteFile`'s create path, `RemoveEntry`, `copyFilesToSubFS`). An index that drifts from `m.files` would be a worse bug than the scan it replaces. Left as O(n): acceptable for the fixture sizes a test mock is expected to hold.

## Benchmarks do not use `b.Loop()`

Go 1.24 introduced `b.Loop()` for benchmarks. Not adopted: the module's Go version floor is `1.22`, and benchmark style should not require a newer Go version than the module itself supports. Revisit if the floor moves to 1.24.
