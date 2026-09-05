# Contributing

## Branches

- `master` — active development.
- `v1` — static pointer to the latest `v1` release.
- `v2` — static pointer to the latest `v2` release.

Work happens on `master`. `v1` and `v2` are not development branches.

## Design Decisions

`DECISIONS.md` records rejected design choices. Check it before you propose an alternative to an existing design. The choice may already have a documented reason.

## Requirements

- Go 1.25 or later, see `go.mod`. This is a floor, not a target. The module carries no upper bound.
- No external dependencies. `depguard`, configured in `.golangci.yml`, enforces this in CI. Only the Go standard library and this module's own packages may be imported.

## Building and testing

The `Makefile` defines common tasks:

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

`golangci-lint` v2 is configured in `.golangci.yml`. It enables 40 linters across:

- Correctness
- Security (`gosec`, full rule set)
- Dependency policy (`depguard`)
- Performance
- Style and naming
- Error handling
- Concurrency
- Testing conventions
- Modern-Go idioms

Every `//nolint` directive must name the specific linter and give a reason. `nolintlint` enforces this:

```go
//nolint:rulename // <reason>
```

## CI

`.github/workflows/go.yml` runs on push and pull request against `master`, `v1`, and `v2`, with these jobs:

- `mod-tidy` — If `go mod tidy` would change `go.mod` or `go.sum`, this job fails.
- `lint` — This job runs `golangci-lint`, pinned to `v2.12.0` for reproducibility.
- `vulncheck` — This job runs `govulncheck`, pinned to `v1.1.4`, against the Go vulnerability database.
- `build` — This job cross-compiles for `linux/arm64`. Tests run on `linux/amd64`. The deployment target is `arm64`.
- `test` — This job runs the matrix across `ubuntu-latest`, `macos-latest`, and `windows-latest`, on both the `go.mod`-pinned and latest stable Go versions. On Linux, tests run with `-race -shuffle=on` and produce a coverage report. The workflow uploads the report to Codecov.
- `ci-gate` — This is a required status check. If any job above failed or was cancelled, it fails.

All jobs must pass before merge.
