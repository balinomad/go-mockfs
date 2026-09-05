# Changelog

All notable changes to `github.com/balinomad/go-mockfs` are documented here, newest first. `v2.x` carries the `/v2` module path. It is a complete rewrite, unrelated to `v1.x` at the API level. See [MIGRATION-v1-to-v2.md](MIGRATION-v1-to-v2.md) to upgrade.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). This project adheres to [Semantic Versioning](https://semver.org/).

## [2.0.0-rc.3] — Unreleased

Stabilization release: breaking API corrections, a full audit of the panic/error boundary, and bug fixes from an API/documentation audit. Several methods changed return type outright, not just gained one. This release also added five new `Must*` constructors and a new `ErrUsage` sentinel.

### Breaking changes

- `FsOption` is now `func(*MockFS) error` (was `func(*MockFS, contextPath string) error`). `File()` and `Dir()` compose exactly as before. A custom `FsOption` implemented as a raw function literal, rather than via `File()`, `Dir()`, or a `With*()` constructor, needs updating.
- `FileOption` is now `func(*fileOptions) error` (was `func(*fileOptions)`). All built-in `WithFile*` functions compose exactly as before. A custom `FileOption` implemented as a raw function literal, rather than via a `WithFile*` constructor, needs updating. `WithFileLatency` and `WithFilePerOperationLatency` now return an error instead of panicking on a negative duration. `NewMockFile` propagates that error instead of letting the panic pass through uncaught.
- `NewErrorRule` now returns `(*ErrorRule, error)` instead of `*ErrorRule`. It no longer panics when `after` is negative for `ErrorModeAfterSuccesses` or `ErrorModeNext` — it returns an error instead.
- `ErrorInjector.AddExact`, `AddAll`, `AddExactForAllOps`, and `AddAllForAllOps` now return `error` (previously no return value). `AddGlob`, `AddRegexp`, `AddGlobForAllOps`, and `AddRegexpForAllOps` keep their existing `error` return but now also report a negative `after`, in addition to a malformed pattern.
- All 24 `MockFS.FailX`/`FailXOnce` convenience methods (e.g. `FailStat`, `FailOpen`, `FailReadAfter`, `FailReadNext`) now return `error`. For methods that hardcode `ErrorModeAlways`/`ErrorModeOnce` with `after=0`, the error is unreachable through them. `FailReadAfter` and `FailReadNext` can genuinely fail if `successes`/`count` is negative.
- `NewMockFS`, `NewMockFile`, `NewLatencySimulator`, `NewLatencySimulatorPerOp`, and `NewFileInfo` now return `(T, error)` instead of panicking on invalid input. `MustNewMockFS`, `MustNewMockFile`, `MustNewLatencySimulator`, `MustNewLatencySimulatorPerOp`, and `MustNewFileInfo` are new counterparts that panic, for callers that want the previous behavior.
- `NewErrorRule` now also validates `mode`: an invalid `ErrorMode` returns an error instead of panicking on first use inside `CheckAndApply`.
- `Stats.FailedOperations()` now returns `iter.Seq[Operation]` instead of `[]Operation`. Collect with `slices.Collect(stats.FailedOperations())` where a `[]Operation` is still needed.
- `ErrorRule.Mode` is now unexported. Use the new `(*ErrorRule).Mode()` getter instead of the `Mode` field. `NewErrorRule` already validated `mode` at construction. As a plain exported field, `Mode` was still directly mutable afterward. This silently bypassed that validation until the corrupted value reached a panic deep inside `CheckAndApply`. Unexporting closes the gap at the type level instead of relying on callers not to do it.

### Added

- `OpenMockFile(name string) (*MockFile, error)` on `MockFS`, which returns the concrete `*MockFile` directly so callers avoid `f.(*mockfs.MockFile)` (`mockfs.go`).
- `ErrUsage`, a sentinel wrapped by errors returned from invalid `File`/`Dir` options, a nil `MapFile`, a negative latency duration, invalid `NewFileInfo` arguments, and invalid `NewErrorRule` mode/`after` values — `errors.Is(err, mockfs.ErrUsage)` (`error.go`).
- `ErrorMode.IsValid()` (`error.go`).

### Fixed

- `Sub(".")` now returns the receiver unchanged instead of `ErrInvalid`. This matches standard library `fs.Sub` (`mockfs.go`).
- `(*MockFile).WriteAt` on a read-only file now applies configured latency before returning the permission error. This matches `Write`'s ordering (`mockfile.go`).
- `(*MockFile).ReadDir` on a `NewMockDir` with a `nil` handler now applies configured latency and error injection before returning, instead of bypassing both (`mockfile.go`).
- `doc.go`'s Statistics example used `file.(mockfs.MockFile)` — a non-pointer type assertion that panics at runtime, since `MockFile` methods use pointer receivers. The example now uses `file.(*mockfs.MockFile)` and shows `OpenMockFile` as the preferred alternative.
- `USAGE.md`'s "Testing Retry Logic" example had the same non-pointer assertion bug. It now uses `OpenMockFile`.
- Injected and sentinel errors returned from `ErrorInjector.CheckAndApply` were incorrectly re-wrapped with a `mockfs: ` prefix, at 16 call sites across `mockfs.go` and `mockfile.go`:
  - `mockfs.go`: `Stat`, `Open`, `ReadDir`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `WriteFile`, `ReadFile`, `failExact`.
  - `mockfile.go`: `WriteAt`, `Seek`, `ReadDir`, `Stat`, `Close`.

  This broke `errors.Is` and exact-message matching for callers. It also failed the package's own runnable Examples. Errors are now returned verbatim.

- `MockFS.ReadFile` unconditionally wrapped its result with `fmt.Errorf("mockfs: %w", err)`. This produced `%!w(<nil>)` on every successful call.
- Serialized-mode latency simulation held an internal mutex across the simulated delay. This is `WithLatency`/`WithPerOperationLatency`, the default and the only mode `MockFS`/`MockFile` use internally. Holding the mutex this way deadlocks under `testing/synctest`: `sync.Mutex` waits are never durably blocking in a bubble. The fix replaces the mutex with a channel-based ticket for serialization. Behavior under real time is unchanged (`latency.go`).
- `(*MockFile)`'s locked methods held `f.mu`, a `sync.Mutex`, across `LatencySimulator.Simulate()`'s sleep. The locked methods are `Read`, `ReadAt`, `Write`, `WriteAt`, `Seek`, `ReadDir`, `Stat`, and `Close`. This is the same `testing/synctest` deadlock class as the `latency.go` fix above. Any consumer that combines configured latency with concurrent calls into the same file handle, inside their own `synctest.Test`, could hang. `f.mu` now uses a channel-based ticket, with the same acquire/hold/release discipline as before. Behavior under real time is unchanged (`mockfile.go`).
- `AddExactForAllOps`, `AddGlobForAllOps`, `AddRegexpForAllOps`, and `AddAllForAllOps` validated `after` but not `mode`. An invalid `ErrorMode` was silently accepted. It surfaced only later, as a panic inside `CheckAndApply` → `shouldReturnError`, instead of failing at the call that misused it. `AddExact`, `AddGlob`, `AddRegexp`, and `AddAll` were unaffected — they validate both via `NewErrorRule`. The fix extracts `validateModeAndAfter`, now used by all eight methods and by `NewErrorRule` itself (`error.go`).

### Changed

- `go.mod` requires `go 1.25`. `go.mod` originally required `go 1.22`, for `bytes.Clone`, the `slices` package, and Go 1.22 loop-variable-capture semantics. The floor was raised before release, because Go 1.22 reached end-of-life with no fix for [GO-2025-3750](https://pkg.go.dev/vuln/GO-2025-3750) (CVE-2025-0913). By release time, `1.23` and `1.24` were also end-of-life, with no fix on either branch.
- CI (`go.yml`) was reworked. The package is now verified across ubuntu/macos/windows, on both the `go.mod`-pinned and latest stable Go versions, plus a linux/arm64 cross-compile check. See `CONTRIBUTING.md` for the full CI, lint, and build setup.
- `collectDirEntries` (`mockfs.go`) was restructured into two deterministic passes. Previously, which of two code paths represented a nested subdirectory in `ReadDir` depended on Go's map iteration order. Output was always correct either way, but the split made line coverage of the subdirectory-resolution path non-deterministic across test runs. The fix is a single, order-independent lookup per unique child name. There is no public API or output change.
- `mockfile_test.go`: six `t.Parallel()` real-time latency assertions (`TestMockFile_LatencyCloning`, `TestMockFile_LatencyReset`, `TestMockFile_LatencyOnceMode`, `TestMockFile_LatencySharedSimulator`, `TestMockFile_WriteAt_LatencyBeforeError`, both `TestFileOptions` latency subtests) moved into `synctest.Test`. This matches the existing `latency_test.go` pattern. They asserted real wall-clock duration against a ±20ms tolerance. Under `-race -shuffle=on` CI load, ordinary scheduler contention exceeded that tolerance. Investigation confirmed scheduler contention, not a logic defect, as the cause of a `TestMockFile_LatencyCloning` failure (`file2 first read: expected duration 50ms (±20ms), got 111.9061ms`). No test behavior or coverage change.
- `error_test.go`: added `TestErrorInjector_InvalidModeAfter`. This closes the coverage gap on all eight `ErrorInjector` validation-error returns (`AddExact`, `AddGlob`, `AddRegexp`, `AddAll`, and their `*ForAllOps` variants) for an invalid `mode` and a negative `after`. Also corrected `TestErrorInjector_AfterParameter`'s doc comment, which referenced a `mustAfter` helper that no longer exists in this codebase.

### Documentation

- `doc.go`: the SubFS section now notes that `Sub(".")` returns the receiver. It documents `OpenMockFile` as the preferred way to reach a file handle's `Stats()`, `ErrorInjector()`, and `LatencySimulator()`. The Panic Policy section no longer lists `NewErrorRule`, which no longer panics.
- `fileinfo.go`: added GoDoc comments to `Mode()` and `ModTime()`.
- `USAGE.md`: added a "Prefer OpenMockFile Over Type Assertions" best practice.
- `doc.go`: the "Panic Policy" section is rewritten to describe the testing-error/usage-error/internal-invariant model and the new `Must*` constructors. `Basic Usage`, `Latency Simulation`, `Write Operations`, `SubFS Support`, and the `Statistics` example are updated to use `MustNewMockFS`.
- `README.md`, `USAGE.md`, `MIGRATION-v1-to-v2.md`: examples updated for the `NewMockFS`/`NewMockFile`/`NewLatencySimulator`/`NewLatencySimulatorPerOp`/`NewFileInfo` signature change.
- `error.go`: `ErrorInjector`'s eight `Add*`/`Add*ForAllOps` method docs now note they return an error for an invalid `mode`, not just a negative `after`.

## [2.0.0-rc.2] - 2025-12-04

### Breaking changes

- `MockFile` changed from an interface to a concrete struct. Custom `MockFile` implementations or interface-specific type assertions need updating.
- Further API renaming for naming consistency. This continues the pass started in rc.1.

### Added

- A tree-builder pattern for `MockFS` initialization via the `File()`/`Dir()` functional options, for fluent, structured directory-hierarchy setup.

### Fixed

- Byte-converter error wrapping: errors during byte conversion are now wrapped correctly. This preserves error chains for debugging.
- General stability and edge-case hardening across filesystem operations.
- File-permission literals updated to modern Go octal prefixes (`0o755` instead of `0755`).

### Changed

- Internal error handling is refactored to use local error aliases. This reduces dependency coupling.
- Significant unit-test-coverage improvements, particularly for error scenarios and statistics tracking.

### Documentation

- `README.md` was significantly reduced in size. Detailed usage moved to a new `USAGE.md`.
- Expanded inline GoDoc comments.

## [2.0.0-rc.1] - 2025-10-31

Initial v2 release candidate: a complete rewrite of `mockfs` with breaking changes across the entire API surface. No compatibility with `v1.x`.

### Breaking changes

- File storage: direct ownership of the file map replaces the embedded `fstest.MapFS`.
- `MockFile` became a complete implementation that manages its own state (position, latency, stats), instead of wrapping `fs.File`.
- Statistics split into filesystem-level (`MockFS.Stats()`) and file-handle-level (`MockFile.Stats()`) tracking. The mutable struct-with-public-fields model was replaced by an immutable snapshot.
- Error injection is redesigned around an `ErrorInjector` interface and a composable `PathMatcher` hierarchy (exact, glob, regexp, wildcard). This replaces direct configuration methods.
- A `WritableFS` interface replaces the write-callback pattern for write operations.
- A `LatencySimulator` interface replaces a single global duration. It adds per-operation durations and serialized/async modes.
- Method renaming: `GetStats()` → `Stats()`, `AddFileString()` → `AddFile()`, `AddDirectory()` → `AddDir()`.
- File-management operations that were previously infallible now return an `error`.
- `Operation` constants were renumbered to accommodate new write-related operations.

### Added

- Glob pattern matching (`path.Match` semantics) alongside exact and regexp path matching, plus a wildcard matcher for universal rules.
- Standalone `MockFile` constructors for testing `io.Reader`/`io.Writer`/`io.Seeker` consumers without a filesystem.
- Success/failure counters and byte-level read/write tracking. `Delta()` and `Equal()` for comparing `Stats` snapshots.
- Full `fs.SubFS` support with automatic error-rule path adjustment and independent per-sub-filesystem statistics.

### Documentation

- `MIGRATION-v1-to-v2.md` added.

## [1.0.2] - 2025-09-10

This release refactored unit tests for readability and maintainability. It made no functional or behavioral change.

## [1.0.1] - 2025-09-08

This release refactored complex functions and unit tests for readability. It made no functional or behavioral change.

## [1.0.0] - 2025-04-30

Initial release.

[2.0.0-rc.3]: https://github.com/balinomad/go-mockfs/compare/v2.0.0-rc.2...v2.0.0-rc.3
[2.0.0-rc.2]: https://github.com/balinomad/go-mockfs/compare/v2.0.0-rc.1...v2.0.0-rc.2
[2.0.0-rc.1]: https://github.com/balinomad/go-mockfs/compare/v1.0.2...v2.0.0-rc.1
[1.0.2]: https://github.com/balinomad/go-mockfs/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/balinomad/go-mockfs/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/balinomad/go-mockfs/releases/tag/v1.0.0
