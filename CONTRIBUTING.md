# Contributing

## Branches

- `master` — active development.
- `v1` — static pointer to the latest `v1` release.
- `v2` — static pointer to the latest `v2` release.

Work happens on `master`. `v1` and `v2` are not development branches.

## Requirements

- Go 1.22 or later (see `go.mod`; this is a floor, not a target — the module carries no upper bound).
- No external dependencies. `depguard`, configured in `.golangci.yml`, enforces this in CI: only the Go standard library and this module's own packages may be imported.

## Building and testing

Common tasks are defined in the `Makefile`:

| Target                          | Purpose                                                                    |
| ------------------------------- | -------------------------------------------------------------------------- |
| `make tidy`                     | Run `go mod tidy`.                                                         |
| `make lint`                     | Run `golangci-lint run ./...`.                                             |
| `make test`                     | Run the test suite with `-race`.                                           |
| `make fulltest`                 | Run the test suite verbosely, with `-race -count=1 -shuffle=on`.           |
| `make bench`                    | Run benchmarks.                                                            |
| `make cover`                    | Generate and print a coverage summary.                                     |
| `make fullcover`                | Generate per-package coverage profiles and an HTML report under `.cover/`. |
| `make cyclo` / `make fullcyclo` | Report cyclomatic complexity (over 10, or all functions).                  |
| `make examples`                 | Run only the runnable `Example*` tests.                                    |

## Linting

`golangci-lint` v2 is configured in `.golangci.yml`, enabling 40 linters across correctness, security (`gosec`, full rule set), dependency policy (`depguard`), performance, style and naming, error handling, concurrency, testing conventions, and modern-Go idioms.

Every `//nolint` directive must name the specific linter and give a reason (`nolintlint` enforces this):

```go
//nolint:rulename // <reason>
```

## CI

`.github/workflows/go.yml` runs on push and pull request against `master`, `v1`, and `v2`, with these jobs:

- `mod-tidy` — fails if `go mod tidy` would change `go.mod` or `go.sum`.
- `lint` — `golangci-lint`, pinned to `v2.2.0` for reproducibility.
- `vulncheck` — `govulncheck`, pinned to `v1.3.0`, against the Go vulnerability database.
- `build` — cross-compiles for `linux/arm64` (tests run on `linux/amd64`; the deployment target is `arm64`).
- `test` — matrix across `ubuntu-latest`, `macos-latest`, and `windows-latest`, on both the `go.mod`-pinned and latest stable Go versions; `-race -shuffle=on` with coverage on Linux, uploaded to Codecov.
- `ci-gate` — required status check; fails if any job above failed or was cancelled.

All jobs must pass before merge.
