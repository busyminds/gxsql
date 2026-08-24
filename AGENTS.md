# gxsql Agent Guide

## Module Boundaries

`github.com/busyminds/gxsql` validates SQL tables through `database/sql`.
Expectations render and execute in the database; the core package has no
concrete-driver dependency. Drivers belong in `integration/`.

- Require Go 1.25.0 or newer.
- `Expectation` is sealed. Use the exported builders; do not implement it
  outside package `gxsql`.
- Callers own the `*sql.DB` and should pass `WithDialect(...)` explicitly.
- Preserve declaration order and collect-all behavior. Policy failures belong in
  the completed `Report`; configuration and execution failures use the returned
  error unless `ContinueOnError()` is selected.

## Documentation

Keep this file to agent workflow. Consumer-facing behavior, examples, support
claims, and API detail belong in `docs/`:

1. `docs/tutorial/` — installation and first validation.
2. `docs/concepts/` — execution, results, operations, export, and compatibility.
3. `docs/reference/` — public API semantics.

Before changing exported behavior, read `doc.go` and the owning documentation
page. Update that page and `README.md` only when its quick-start contract
changes. Do not duplicate detailed public semantics here.

## Layout

| Path                    | Role                                                |
| ----------------------- | --------------------------------------------------- |
| `*.go`                  | Public API and implementation                       |
| `gxsqltest/`            | `Check` and `Require` test helpers                  |
| `integration/`          | Separate real-engine conformance module and drivers |
| `internal/conformance/` | Shared real-engine contract runner                  |
| `docs/`                 | Consumer tutorial, concepts, and reference          |

## Tests and Commands

Run commands from the module root.

- `make test` — race-enabled unit tests.
- `make integration-test` — real-engine conformance tests.
- `make check` — CI gate.
- `make fmt-check` — formatting check.

Unit tests use the fake driver in `harness_test.go` (`setHarnessData`,
`openHarnessDB`, and `harnessUsers`). Pass `WithDialect(Postgres())` unless
testing a different dialect. Consumer tests should use `gxsqltest.Check` or
`gxsqltest.Require`.

For behavior changes, add a focused test first; preserve report shape, error
categories, and wording unless the change intentionally updates that contract.
Run the narrowest relevant race-enabled test before yielding.
