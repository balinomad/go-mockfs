# Changelog

All notable changes to `github.com/balinomad/go-mockfs` are documented here, newest first. `v2.x` carries the `/v2` module path and is a complete rewrite, unrelated to `v1.x` at the API level — see [MIGRATION-v1-to-v2.md](MIGRATION-v1-to-v2.md) for upgrading.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [2.0.0-rc.3] — Unreleased

Stabilization release: breaking API corrections and bug fixes from an API/documentation audit. No methods removed; several gained a new return value.

### Breaking changes

- `FsOption` is now `func(*MockFS) error` (was `func(*MockFS, contextPath string) error`). `File()` and `Dir()` compose exactly as before; any custom `FsOption` implemented as a raw function literal, rather than via `File()`, `Dir()`, or a `With*()` constructor, needs updating.
- `NewErrorRule` now returns `(*ErrorRule, error)` instead of `*ErrorRule`, and no longer panics when `after` is negative for `ErrorModeAfterSuccesses`/`ErrorModeNext` — it returns an error instead.
- `ErrorInjector.AddExact`, `AddAll`, `AddExactForAllOps`, and `AddAllForAllOps` now return `error` (previously no return value). `AddGlob`, `AddRegexp`, `AddGlobForAllOps`, and `AddRegexpForAllOps` keep their existing `error` return but now also report a negative `after`, in addition to a malformed pattern.
- All 24 `MockFS.FailX`/`FailXOnce` convenience methods (e.g. `FailStat`, `FailOpen`, `FailReadAfter`, `FailReadNext`) now return `error`. For methods that hardcode `ErrorModeAlways`/`ErrorModeOnce` with `after=0`, the error is unreachable through them; `FailReadAfter` and `FailReadNext` can genuinely fail if `successes`/`count` is negative.

### Added

- `OpenMockFile(name string) (*MockFile, error)` on `MockFS`, returning the concrete `*MockFile` directly so callers avoid `f.(*mockfs.MockFile)` (`mockfs.go`).

### Fixed

- `Sub(".")` now returns the receiver unchanged instead of `ErrInvalid`, matching stdlib `fs.Sub` (`mockfs.go`).
- `(*MockFile).WriteAt` on a read-only file now applies configured latency before returning the permission error, matching `Write`'s ordering (`mockfile.go`).
- `(*MockFile).ReadDir` on a `NewMockDir` with a `nil` handler now applies configured latency and error injection before returning, instead of bypassing both (`mockfile.go`).
- `doc.go`'s Statistics example used `file.(mockfs.MockFile)` — a non-pointer type assertion that panics at runtime, since `MockFile` methods use pointer receivers. Corrected to `file.(*mockfs.MockFile)`, with `OpenMockFile` now shown as the preferred alternative.
- `USAGE.md`'s "Testing Retry Logic" example had the same non-pointer assertion bug; corrected to use `OpenMockFile`.

### Changed

- `go.mod` requires `go 1.22` (uses `bytes.Clone`, the `slices` package, and Go 1.22 loop-variable-capture semantics).
- CI (`go.yml`) reworked: the package is now verified across ubuntu/macos/windows on both the `go.mod`-pinned and latest stable Go versions, plus a linux/arm64 cross-compile check. See `CONTRIBUTING.md` for the full CI, lint, and build setup.

### Documentation

- `doc.go`: SubFS section notes that `Sub(".")` returns the receiver; documents `OpenMockFile` as the preferred way to reach a file handle's `Stats()`, `ErrorInjector()`, and `LatencySimulator()`; the Panic Policy section no longer lists `NewErrorRule`, which no longer panics.
- `fileinfo.go`: added GoDoc comments to `Mode()` and `ModTime()`.
- `USAGE.md`: added a "Prefer OpenMockFile Over Type Assertions" best practice.

## [2.0.0-rc.2] 2025-12-04

### Breaking changes

- `MockFile` changed from an interface to a concrete struct. Custom `MockFile` implementations or interface-specific type assertions need updating.
- Further API renaming for naming consistency, continuing the pass started in rc.1.

### Added

- A tree-builder pattern for `MockFS` initialization via the `File()`/`Dir()` functional options, for fluent, structured directory-hierarchy setup.

### Fixed

- Byte-converter error wrapping: errors during byte conversion are now wrapped correctly, preserving error chains for debugging.
- General stability and edge-case hardening across filesystem operations.
- File-permission literals updated to modern Go octal prefixes (`0o755` instead of `0755`).

### Changed

- Internal error handling refactored to use local error aliases, reducing dependency coupling.
- Significant unit-test-coverage improvements, particularly for error scenarios and statistics tracking.

### Documentation

- `README.md` significantly reduced in size; detailed usage moved to a new `USAGE.md`.
- Expanded inline GoDoc comments.

## [2.0.0-rc.1] - 2025-10-31

Initial v2 release candidate: a complete rewrite of `mockfs` with breaking changes across the entire API surface. No compatibility with `v1.x`.

### Breaking changes

- File storage: direct ownership of the file map replaces the embedded `fstest.MapFS`.
- `MockFile` became a complete implementation managing its own state (position, latency, stats) instead of wrapping `fs.File`.
- Statistics split into filesystem-level (`MockFS.Stats()`) and file-handle-level (`MockFile.Stats()`) tracking; the mutable struct-with-public-fields model was replaced by an immutable snapshot.
- Error injection redesigned around an `ErrorInjector` interface and a composable `PathMatcher` hierarchy (exact, glob, regexp, wildcard), replacing direct configuration methods.
- A `WritableFS` interface replaces the write-callback pattern for write operations.
- A `LatencySimulator` interface replaces a single global duration, adding per-operation durations and serialized/async modes.
- Method renaming: `GetStats()` → `Stats()`, `AddFileString()` → `AddFile()`, `AddDirectory()` → `AddDir()`.
- File-management operations that were previously infallible now return an `error`.
- `Operation` constants renumbered to accommodate new write-related operations.

### Added

- Glob pattern matching (`path.Match` semantics) alongside exact and regexp path matching, plus a wildcard matcher for universal rules.
- Standalone `MockFile` constructors for testing `io.Reader`/`io.Writer`/`io.Seeker` consumers without a filesystem.
- Success/failure counters and byte-level read/write tracking; `Delta()` and `Equal()` for comparing `Stats` snapshots.
- Full `fs.SubFS` support with automatic error-rule path adjustment and independent per-sub-filesystem statistics.

### Documentation

- `MIGRATION-v1-to-v2.md` added.

## [1.0.2] - 2025-09-10

Refactored unit tests for readability and maintainability. No functional or behavioral change.

## [1.0.1] - 2025-09-08

Refactored complex functions and unit tests for readability. No functional or behavioral change.

## [1.0.0] - 2025-04-30

Initial release.

[2.0.0-rc.3]: https://github.com/balinomad/go-mockfs/compare/v2.0.0-rc.2...v2.0.0-rc.3
[2.0.0-rc.2]: https://github.com/balinomad/go-mockfs/compare/v2.0.0-rc.1...v2.0.0-rc.2
[2.0.0-rc.1]: https://github.com/balinomad/go-mockfs/compare/v1.0.2...v2.0.0-rc.1
[1.0.2]: https://github.com/balinomad/go-mockfs/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/balinomad/go-mockfs/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/balinomad/go-mockfs/releases/tag/v1.0.0
